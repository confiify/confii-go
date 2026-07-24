// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package loader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/BurntSushi/toml"
	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/internal/formatparse"
	"gopkg.in/yaml.v3"
)

// HTTPLoader loads configuration from an HTTP/HTTPS endpoint.
type HTTPLoader struct {
	url     string
	timeout time.Duration
	headers map[string]string
	auth    *BasicAuth
}

// BasicAuth holds HTTP basic auth credentials.
type BasicAuth struct {
	Username string
	Password string
}

// HTTPOption configures the HTTPLoader.
type HTTPOption func(*HTTPLoader)

// WithTimeout sets the HTTP request timeout.
func WithTimeout(d time.Duration) HTTPOption {
	return func(l *HTTPLoader) { l.timeout = d }
}

// WithHeaders sets HTTP headers for the request.
func WithHeaders(h map[string]string) HTTPOption {
	return func(l *HTTPLoader) { l.headers = h }
}

// WithBasicAuth sets HTTP basic auth credentials.
func WithBasicAuth(username, password string) HTTPOption {
	return func(l *HTTPLoader) { l.auth = &BasicAuth{Username: username, Password: password} }
}

// NewHTTP creates a new HTTP loader for the given URL.
func NewHTTP(url string, opts ...HTTPOption) *HTTPLoader {
	l := &HTTPLoader{
		url:     url,
		timeout: 30 * time.Second,
		headers: make(map[string]string),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Source returns the identifier for this loader's configuration source.
func (l *HTTPLoader) Source() string { return l.url }

// Load fetches configuration from the HTTP/HTTPS endpoint at the configured URL and parses the response body.
func (l *HTTPLoader) Load(ctx context.Context) (map[string]any, error) {
	client := &http.Client{Timeout: l.timeout}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.url, nil)
	if err != nil {
		return nil, confii.NewLoadError(l.url, err)
	}

	for k, v := range l.headers {
		req.Header.Set(k, v)
	}
	if l.auth != nil {
		req.SetBasicAuth(l.auth.Username, l.auth.Password)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, confii.NewLoadError(l.url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, confii.NewLoadError(l.url, fmt.Errorf("HTTP %d", resp.StatusCode))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, confii.NewLoadError(l.url, err)
	}

	// Detect format from Content-Type, then URL extension. When neither is
	// useful, ParseContent performs deterministic JSON-then-YAML detection.
	format := formatparse.FromContentType(resp.Header.Get("Content-Type"))
	if format == formatparse.FormatUnknown {
		format = formatparse.FromExtension(l.url)
	}
	return ParseContent(body, format, l.url)
}

// ParseContent parses raw bytes into a config map based on format.
// Exported for use by cloud loaders.
//
// Supported formats:
//   - [formatparse.FormatJSON]: parsed via [encoding/json.Unmarshal].
//   - [formatparse.FormatYAML]: parsed via [gopkg.in/yaml.v3.Unmarshal].
//   - [formatparse.FormatTOML]: parsed via [github.com/BurntSushi/toml.Unmarshal]
//     (G19: previously the TOML format was detected by [HTTPLoader] but had no
//     parsing branch; it now reuses the same TOML library backing
//     [TOMLLoader.Load], so HTTP and cloud loaders share a single parsing path).
//   - [formatparse.FormatUnknown]: tries JSON first, then YAML. This preserves
//     JSON's stricter interpretation for ambiguous payloads while supporting
//     YAML endpoints that omit both Content-Type and a recognizable extension.
func ParseContent(data []byte, format formatparse.Format, source string) (map[string]any, error) {
	var result map[string]any
	var err error

	switch format {
	case formatparse.FormatJSON:
		err = json.Unmarshal(data, &result)
	case formatparse.FormatYAML:
		err = yaml.Unmarshal(data, &result)
	case formatparse.FormatTOML:
		err = toml.Unmarshal(data, &result)
	case formatparse.FormatUnknown:
		jsonErr := json.Unmarshal(data, &result)
		if jsonErr == nil {
			return result, nil
		}
		result = nil
		if yamlErr := yaml.Unmarshal(data, &result); yamlErr != nil {
			err = fmt.Errorf("JSON parse: %v; YAML parse: %w", jsonErr, yamlErr)
		}
	default:
		err = fmt.Errorf("unsupported format %q", format)
	}

	if err != nil {
		return nil, confii.NewFormatError(source, string(format), err)
	}
	return result, nil
}
