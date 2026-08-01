// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package hook

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const defaultFileResolverMaxBytes int64 = 8 << 20

var filePattern = regexp.MustCompile(`\$\{file:([^}]+)\}`)

// FileResolverOption configures NewFileResolverHook.
type FileResolverOption func(*fileResolverOptions)

type fileResolverOptions struct {
	maxBytes int64
}

// WithFileResolverMaxBytes bounds the size of each file resolved by
// NewFileResolverHook. A value below one rejects all file placeholders.
func WithFileResolverMaxBytes(n int64) FileResolverOption {
	return func(o *fileResolverOptions) {
		o.maxBytes = n
	}
}

// NewFileResolverHook returns a hook that replaces ${file:path} placeholders
// with UTF-8 file contents rooted at baseDir. Relative paths are resolved from
// baseDir. Absolute paths and symlinks are allowed only when their final target
// remains inside baseDir. Missing, oversized, non-regular, or escaping files
// return an error and leave the original string unpublished.
func NewFileResolverHook(baseDir string, opts ...FileResolverOption) Func {
	if baseDir == "" {
		baseDir = "."
	}
	options := fileResolverOptions{maxBytes: defaultFileResolverMaxBytes}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}

	return func(ctx context.Context, _ string, value any) (any, error) {
		if ctx == nil {
			return value, fmt.Errorf("file resolver: nil context")
		}
		if err := ctx.Err(); err != nil {
			return value, err
		}
		s, ok := value.(string)
		if !ok || !strings.Contains(s, "${file:") {
			return value, nil
		}

		var lastErr error
		result := filePattern.ReplaceAllStringFunc(s, func(match string) string {
			if lastErr != nil {
				return match
			}
			groups := filePattern.FindStringSubmatch(match)
			content, err := readResolvedFile(ctx, baseDir, groups[1], options.maxBytes)
			if err != nil {
				lastErr = err
				return match
			}
			return content
		})
		if lastErr != nil {
			return value, lastErr
		}
		return result, nil
	}
}

func readResolvedFile(ctx context.Context, baseDir, requested string, maxBytes int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path := strings.TrimSpace(requested)
	if path == "" {
		return "", fmt.Errorf("file resolver: empty path")
	}
	if strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("file resolver: path contains NUL")
	}
	if maxBytes < 1 {
		return "", fmt.Errorf("file resolver: max bytes must be at least 1")
	}

	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("file resolver: resolve base directory %q: %w", baseDir, err)
	}
	target := path
	if !filepath.IsAbs(target) {
		target = filepath.Join(baseAbs, target)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("file resolver: resolve path %q: %w", path, err)
	}

	baseReal, err := filepath.EvalSymlinks(baseAbs)
	if err != nil {
		return "", fmt.Errorf("file resolver: resolve base directory %q: %w", baseDir, err)
	}
	targetReal, err := filepath.EvalSymlinks(targetAbs)
	if err != nil {
		return "", fmt.Errorf("file resolver: resolve path %q: %w", path, err)
	}
	if !pathWithinBase(baseReal, targetReal) {
		return "", fmt.Errorf("file resolver: path %q escapes base directory", path)
	}

	info, err := os.Stat(targetReal)
	if err != nil {
		return "", fmt.Errorf("file resolver: stat %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("file resolver: %q is not a regular file", path)
	}
	if info.Size() > maxBytes {
		return "", fmt.Errorf("file resolver: %q exceeds %d bytes", path, maxBytes)
	}

	data, err := os.ReadFile(targetReal) // #nosec G304 -- path is constrained to baseDir above.
	if err != nil {
		return "", fmt.Errorf("file resolver: read %q: %w", path, err)
	}
	return string(data), nil
}

func pathWithinBase(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}
