// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package hook

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReferenceResolverStructuredFileAndSelf(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.json"), []byte(`{"nested":{"value":42}}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.yaml"), []byte("nested:\n  value: yaml-value\n"), 0o600))

	h := NewReferenceResolverHook(map[string]ResolverFunc{
		"json": NewJSONReferenceResolver(dir, 8<<20),
		"yaml": NewYAMLReferenceResolver(dir, 8<<20),
	})
	ctx := WithResolverSelf(context.Background(), map[string]any{"nested": map[string]any{"value": "self-value"}})

	got, err := h(ctx, "key", "${json:shared.json#nested.value}")
	require.NoError(t, err)
	assert.Equal(t, float64(42), got)

	got, err = h(ctx, "key", "${yaml:shared.yaml#nested.value}")
	require.NoError(t, err)
	assert.Equal(t, "yaml-value", got)

	got, err = h(ctx, "key", "${json:self#nested.value}")
	require.NoError(t, err)
	assert.Equal(t, "self-value", got)

	got, err = h(ctx, "key", "${json:shared.json}")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"nested": map[string]any{"value": float64(42)}}, got)

	got, err = h(ctx, "key", "prefix ${json:shared.json#nested.value}")
	require.NoError(t, err)
	assert.Equal(t, "prefix 42", got)

	got, err = h(ctx, "key", "${json:missing#nested.value}")
	require.Error(t, err)
	assert.Equal(t, "${json:missing#nested.value}", got)
}

func TestReferenceResolverURLAndCommand(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "https://example.test/value", req.URL.String())
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("url-body")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}
	h := NewReferenceResolverHook(map[string]ResolverFunc{
		"url": NewURLReferenceResolver(client, 8<<20),
		"cmd": NewCommandReferenceResolver(0, 8<<20),
	})

	got, err := h(context.Background(), "key", "${url:https://example.test/value}")
	require.NoError(t, err)
	assert.Equal(t, "url-body", got)

	got, err = h(context.Background(), "key", "${cmd:printf cmd-body}")
	require.NoError(t, err)
	assert.Equal(t, "cmd-body", got)
}

func TestFileReferenceResolver(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "value.txt"), []byte("file-body"), 0o600))

	resolver := NewFileReferenceResolver(dir, 8<<20)
	got, err := resolver(context.Background(), ResolverRequest{Target: "value.txt"})
	require.NoError(t, err)
	assert.Equal(t, "file-body", got)
}

func TestReferenceResolverPassthroughAndContextErrors(t *testing.T) {
	h := NewReferenceResolverHook(map[string]ResolverFunc{
		"custom": func(_ context.Context, req ResolverRequest) (any, error) {
			assert.Equal(t, ResolverRequest{Scheme: "custom", Target: "target", Fragment: "field", Key: "key"}, req)
			return "resolved", nil
		},
	})

	got, err := h(context.Background(), "key", 42)
	require.NoError(t, err)
	assert.Equal(t, 42, got)

	got, err = h(context.Background(), "key", "plain")
	require.NoError(t, err)
	assert.Equal(t, "plain", got)

	got, err = h(context.Background(), "key", "${}")
	require.NoError(t, err)
	assert.Equal(t, "${}", got)

	got, err = h(context.Background(), "key", "${unknown:value}")
	require.NoError(t, err)
	assert.Equal(t, "${unknown:value}", got)

	got, err = h(context.Background(), "key", "before ${unknown:value} after")
	require.NoError(t, err)
	assert.Equal(t, "before ${unknown:value} after", got)

	got, err = h(context.Background(), "key", "${custom:target#field}")
	require.NoError(t, err)
	assert.Equal(t, "resolved", got)

	var nilContext context.Context
	got, err = h(nilContext, "key", "${custom:target}")
	require.Error(t, err)
	assert.Equal(t, "${custom:target}", got)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	got, err = h(canceled, "key", "${custom:target}")
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, "${custom:target}", got)
}

func TestReferenceResolverEmbeddedResolverErrorLeavesValueUnchanged(t *testing.T) {
	boom := errors.New("resolver failed")
	h := NewReferenceResolverHook(map[string]ResolverFunc{
		"custom": func(context.Context, ResolverRequest) (any, error) {
			return nil, boom
		},
	})

	got, err := h(context.Background(), "key", "before ${custom:target} after")
	require.ErrorIs(t, err, boom)
	assert.Equal(t, "before ${custom:target} after", got)
}

func TestStructuredReferenceResolverErrors(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "invalid.json"), []byte(`{"bad"`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scalar.json"), []byte(`42`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "object.json"), []byte(`{"present":true}`), 0o600))

	h := NewReferenceResolverHook(map[string]ResolverFunc{
		"json": NewJSONReferenceResolver(dir, 8<<20),
		"yaml": NewYAMLReferenceResolver(dir, 8<<20),
	})

	tests := []struct {
		name  string
		input string
		ctx   context.Context
		want  string
	}{
		{"missing self", "${json:self#value}", context.Background(), "self snapshot is unavailable"},
		{"parse failure", "${json:invalid.json#value}", context.Background(), "parse referenced document"},
		{"scalar field", "${json:scalar.json#value}", context.Background(), "cannot select field"},
		{"missing field", "${json:object.json#missing}", context.Background(), "field \"missing\" not found"},
		{"unknown structured format", "${yaml:self#value}", WithResolverSelf(context.Background(), map[string]any{"value": "ok"}), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := h(tt.ctx, "key", tt.input)
			if tt.want == "" {
				require.NoError(t, err)
				assert.Equal(t, "ok", got)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.Equal(t, tt.input, got)
		})
	}

	_, err := decodeStructuredReference([]byte("{}"), "toml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")

	_, err = decodeStructuredReference([]byte("? null\n: value\n"), "yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "null map key")
}

func TestURLReferenceResolverErrors(t *testing.T) {
	t.Run("fragment is preserved", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, "https://example.test/value#section", req.URL.String())
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("ok")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})}
		resolver := NewURLReferenceResolver(client, 8<<20)
		got, err := resolver(context.Background(), ResolverRequest{Target: "https://example.test/value", Fragment: "section"})
		require.NoError(t, err)
		assert.Equal(t, "ok", got)
	})

	t.Run("parse failure", func(t *testing.T) {
		resolver := NewURLReferenceResolver(nil, 8<<20)
		_, err := resolver(context.Background(), ResolverRequest{Target: "http://[::1"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse")
	})

	t.Run("unsupported scheme", func(t *testing.T) {
		resolver := NewURLReferenceResolver(nil, 8<<20)
		_, err := resolver(context.Background(), ResolverRequest{Target: "file:///tmp/value"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported scheme")
	})

	t.Run("http status", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTeapot,
				Body:       io.NopCloser(strings.NewReader("nope")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})}
		resolver := NewURLReferenceResolver(client, 8<<20)
		_, err := resolver(context.Background(), ResolverRequest{Target: "https://example.test/nope"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HTTP 418")
	})

	t.Run("client error", func(t *testing.T) {
		boom := errors.New("network failed")
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, boom
		})}
		resolver := NewURLReferenceResolver(client, 8<<20)
		_, err := resolver(context.Background(), ResolverRequest{Target: "https://example.test/nope"})
		require.ErrorIs(t, err, boom)
	})

	t.Run("oversized body", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("toolong")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})}
		resolver := NewURLReferenceResolver(client, 3)
		_, err := resolver(context.Background(), ResolverRequest{Target: "https://example.test/large"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds 3 bytes")
	})
}

func TestCommandReferenceResolverErrors(t *testing.T) {
	resolver := NewCommandReferenceResolver(0, 8<<20)
	_, err := resolver(context.Background(), ResolverRequest{Target: "   "})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty command")

	resolver = NewCommandReferenceResolver(0, 3)
	_, err = resolver(context.Background(), ResolverRequest{Target: "printf toolong"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "output exceeds 3 bytes")

	resolver = NewCommandReferenceResolver(0, 8<<20)
	_, err = resolver(context.Background(), ResolverRequest{Target: "exit 7"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command failed")

	_, err = resolver(context.Background(), ResolverRequest{Target: commandWithStderrFailure()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stderr")

	resolver = NewCommandReferenceResolver(time.Nanosecond, 8<<20)
	_, err = resolver(context.Background(), ResolverRequest{Target: slowCommand()})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestReadBoundedAndLimitedBufferErrors(t *testing.T) {
	_, err := readBounded(strings.NewReader("value"), 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max bytes")

	_, err = readBounded(errReader{}, 8)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read failed")

	var stdout limitedBuffer
	stdout.limit = 0
	_, err = stdout.Write([]byte("value"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max bytes")

	stdout.limit = 3
	_, err = stdout.Write([]byte("toolong"))
	require.Error(t, err)
	assert.Equal(t, "too", stdout.String())

	stdout = limitedBuffer{limit: 8}
	_, err = stdout.Write([]byte("ok"))
	require.NoError(t, err)
	assert.Equal(t, "ok", stdout.String())

	data, err := readBounded(bytes.NewBufferString("ok"), 8)
	require.NoError(t, err)
	assert.Equal(t, []byte("ok"), data)
}

func TestDecodeStructuredReferenceYAMLParseFailure(t *testing.T) {
	_, err := decodeStructuredReference([]byte("key: [unterminated"), "yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse referenced document")
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func commandWithStderrFailure() string {
	if runtime.GOOS == "windows" {
		return "echo stderr 1>&2 && exit /b 7"
	}
	return "echo stderr >&2; exit 7"
}

func slowCommand() string {
	if runtime.GOOS == "windows" {
		return "ping -n 2 127.0.0.1 >NUL"
	}
	return "sleep 1"
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
