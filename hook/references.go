// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/confiify/confii-go/v2/configmap"
	"github.com/confiify/confii-go/v2/internal/dictutil"
	"go.yaml.in/yaml/v3"
)

const (
	defaultReferenceResolverMaxBytes = 8 << 20
	defaultURLResolverTimeout        = 30 * time.Second
	defaultCommandResolverTimeout    = 10 * time.Second
)

var referencePattern = regexp.MustCompile(`\$\{([A-Za-z][A-Za-z0-9_-]*):([^}]*)\}`)

type resolverSelfContextKey struct{}

// ResolverRequest describes one ${scheme:target#field} placeholder.
type ResolverRequest struct {
	Scheme   string
	Target   string
	Fragment string
	Key      string
}

// ResolverFunc resolves one placeholder body. It may return any Go value. When
// the placeholder is the complete input string, that value is returned as-is;
// when the placeholder is embedded inside a larger string, fmt.Sprint(value) is
// spliced into the surrounding text.
type ResolverFunc func(context.Context, ResolverRequest) (any, error)

// WithResolverSelf returns a context that lets json:self#path and yaml:self#path
// resolvers read from the current unresolved configuration snapshot.
func WithResolverSelf(ctx context.Context, root map[string]any) context.Context {
	return context.WithValue(ctx, resolverSelfContextKey{}, root)
}

// NewReferenceResolverHook returns a hook for ${scheme:target#field}
// placeholders. Only schemes present in resolvers are handled; unknown schemes
// are left unchanged for later hooks or application code.
func NewReferenceResolverHook(resolvers map[string]ResolverFunc) Func {
	owned := make(map[string]ResolverFunc, len(resolvers))
	for scheme, resolver := range resolvers {
		scheme = strings.ToLower(strings.TrimSpace(scheme))
		if scheme != "" && resolver != nil {
			owned[scheme] = resolver
		}
	}

	return func(ctx context.Context, key string, value any) (any, error) {
		if ctx == nil {
			return value, errors.New("reference resolver: nil context")
		}
		if err := ctx.Err(); err != nil {
			return value, err
		}
		s, ok := value.(string)
		if !ok || !strings.Contains(s, "${") {
			return value, nil
		}

		matches := referencePattern.FindAllStringSubmatchIndex(s, -1)
		if len(matches) == 0 {
			return value, nil
		}

		if len(matches) == 1 && matches[0][0] == 0 && matches[0][1] == len(s) {
			req, resolver, ok := resolverForMatch(s, matches[0], key, owned)
			if !ok {
				return value, nil
			}
			resolved, err := resolver(ctx, req)
			if err != nil {
				return value, err
			}
			return dictutil.DeepCopyValue(resolved), nil
		}

		var out strings.Builder
		out.Grow(len(s))
		cursor := 0
		for _, match := range matches {
			req, resolver, ok := resolverForMatch(s, match, key, owned)
			if !ok {
				continue
			}
			resolved, err := resolver(ctx, req)
			if err != nil {
				return value, err
			}
			out.WriteString(s[cursor:match[0]])
			_, _ = fmt.Fprint(&out, resolved)
			cursor = match[1]
		}
		if cursor == 0 {
			return value, nil
		}
		out.WriteString(s[cursor:])
		return out.String(), nil
	}
}

// NewFileReferenceResolver returns a resolver for ${file:path}. It includes raw
// file text rooted at baseDir.
func NewFileReferenceResolver(baseDir string, maxBytes int64) ResolverFunc {
	return func(ctx context.Context, req ResolverRequest) (any, error) {
		return readResolvedFile(ctx, baseDir, req.Target, maxBytes)
	}
}

// NewJSONReferenceResolver returns a resolver for ${json:path#field} and
// ${json:self#field}. File paths are rooted at baseDir. The special target
// "self" reads from the current unresolved configuration snapshot.
func NewJSONReferenceResolver(baseDir string, maxBytes int64) ResolverFunc {
	return structuredReferenceResolver(baseDir, maxBytes, "json")
}

// NewYAMLReferenceResolver returns a resolver for ${yaml:path#field} and
// ${yaml:self#field}. File paths are rooted at baseDir. The special target
// "self" reads from the current unresolved configuration snapshot.
func NewYAMLReferenceResolver(baseDir string, maxBytes int64) ResolverFunc {
	return structuredReferenceResolver(baseDir, maxBytes, "yaml")
}

// NewURLReferenceResolver returns a resolver for ${url:https://...}. Only http
// and https URLs are accepted. The response body is returned as raw text.
func NewURLReferenceResolver(client *http.Client, maxBytes int64) ResolverFunc {
	if client == nil {
		client = &http.Client{Timeout: defaultURLResolverTimeout}
	}
	return func(ctx context.Context, req ResolverRequest) (any, error) {
		target := strings.TrimSpace(req.Target)
		if req.Fragment != "" {
			target += "#" + req.Fragment
		}
		parsed, err := url.Parse(target)
		if err != nil {
			return nil, fmt.Errorf("url resolver: parse %q: %w", req.Target, err)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, fmt.Errorf("url resolver: unsupported scheme %q", parsed.Scheme)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("url resolver: build request: %w", err)
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("url resolver: fetch %q: %w", req.Target, err)
		}
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, fmt.Errorf("url resolver: fetch %q: HTTP %d", req.Target, response.StatusCode)
		}
		data, err := readBounded(response.Body, maxBytes)
		if err != nil {
			return nil, fmt.Errorf("url resolver: read %q: %w", req.Target, err)
		}
		return string(data), nil
	}
}

// NewCommandReferenceResolver returns a resolver for ${cmd:command}. The
// command is executed through the platform shell and stdout is returned. This
// is intentionally powerful and should only be enabled for fully trusted config.
func NewCommandReferenceResolver(timeout time.Duration, maxBytes int64) ResolverFunc {
	if timeout == 0 {
		timeout = defaultCommandResolverTimeout
	}
	return func(ctx context.Context, req ResolverRequest) (any, error) {
		if timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		command := strings.TrimSpace(req.Target)
		if command == "" {
			return nil, errors.New("cmd resolver: empty command")
		}
		name, args := shellCommand(command)
		cmd := exec.CommandContext(ctx, name, args...)
		var stdout limitedBuffer
		stdout.limit = maxBytes
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			detail := strings.TrimSpace(stderr.String())
			if detail != "" {
				return nil, fmt.Errorf("cmd resolver: command failed: %w: %s", err, detail)
			}
			return nil, fmt.Errorf("cmd resolver: command failed: %w", err)
		}
		return stdout.String(), nil
	}
}

func structuredReferenceResolver(baseDir string, maxBytes int64, format string) ResolverFunc {
	return func(ctx context.Context, req ResolverRequest) (any, error) {
		var root any
		if strings.EqualFold(strings.TrimSpace(req.Target), "self") {
			current, ok := ctx.Value(resolverSelfContextKey{}).(map[string]any)
			if !ok || current == nil {
				return nil, fmt.Errorf("%s resolver: self snapshot is unavailable", format)
			}
			root = current
		} else {
			content, err := readResolvedFile(ctx, baseDir, req.Target, maxBytes)
			if err != nil {
				return nil, err
			}
			decoded, err := decodeStructuredReference([]byte(content), format)
			if err != nil {
				return nil, err
			}
			root = decoded
		}

		if req.Fragment == "" {
			return dictutil.DeepCopyValue(root), nil
		}
		mapping, ok := root.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s resolver: cannot select field %q from %T", format, req.Fragment, root)
		}
		value, ok := configmap.Get(mapping, req.Fragment)
		if !ok {
			return nil, fmt.Errorf("%s resolver: field %q not found", format, req.Fragment)
		}
		return dictutil.DeepCopyValue(value), nil
	}
}

func decodeStructuredReference(data []byte, format string) (any, error) {
	var raw any
	switch format {
	case "json":
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("json resolver: parse referenced document: %w", err)
		}
	case "yaml":
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("yaml resolver: parse referenced document: %w", err)
		}
	default:
		return nil, fmt.Errorf("structured resolver: unsupported format %q", format)
	}
	normalized, err := dictutil.NormalizeKeys(raw)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

func resolverForMatch(input string, match []int, key string, resolvers map[string]ResolverFunc) (ResolverRequest, ResolverFunc, bool) {
	scheme := strings.ToLower(input[match[2]:match[3]])
	resolver := resolvers[scheme]
	if resolver == nil {
		return ResolverRequest{}, nil, false
	}
	body := input[match[4]:match[5]]
	target, fragment, _ := strings.Cut(body, "#")
	return ResolverRequest{
		Scheme:   scheme,
		Target:   target,
		Fragment: fragment,
		Key:      key,
	}, resolver, true
}

func readBounded(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes < 1 {
		return nil, errors.New("max bytes must be at least 1")
	}
	var buffer bytes.Buffer
	_, err := io.Copy(&buffer, io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(buffer.Len()) > maxBytes {
		return nil, fmt.Errorf("content exceeds %d bytes", maxBytes)
	}
	return buffer.Bytes(), nil
}

func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}
	return "/bin/sh", []string{"-c", command}
}

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int64
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit < 1 {
		return 0, errors.New("cmd resolver: max bytes must be at least 1")
	}
	if int64(b.buffer.Len()+len(p)) > b.limit {
		remaining := int(b.limit) - b.buffer.Len()
		if remaining > 0 {
			_, _ = b.buffer.Write(p[:remaining])
		}
		return 0, fmt.Errorf("cmd resolver: output exceeds %d bytes", b.limit)
	}
	return b.buffer.Write(p)
}

func (b *limitedBuffer) String() string {
	return b.buffer.String()
}
