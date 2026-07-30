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
	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/internal/formatparse"
	"gopkg.in/yaml.v3"
)

// HTTPLoader loads configuration from an HTTP/HTTPS endpoint.
type HTTPLoader struct {
	url     string
	timeout time.Duration
	headers map[string]string
	auth    *BasicAuth
}

// BasicAuth holds HTTP Basic Authentication credentials. Values are kept in
// memory for the lifetime of the loader and must not be included in logs or a
// loader's source identifier.
type BasicAuth struct {
	// Username is the Basic Authentication user name.
	Username string
	// Password is the Basic Authentication password.
	Password string
}

// HTTPOption configures the HTTPLoader.
type HTTPOption func(*HTTPLoader)

// WithTimeout sets the complete HTTP request timeout, including connection,
// response headers, and body reading. NewHTTP defaults to 30 seconds. A
// non-positive duration disables http.Client's timeout; callers should then
// provide a context deadline.
func WithTimeout(d time.Duration) HTTPOption {
	return func(l *HTTPLoader) { l.timeout = d }
}

// WithHeaders replaces the request headers. The map is copied when the option
// is applied, so later caller mutation does not affect the loader. Do not place
// secrets in header values if the loader may be retained longer than needed.
func WithHeaders(h map[string]string) HTTPOption {
	return func(l *HTTPLoader) {
		l.headers = make(map[string]string, len(h))
		for key, value := range h {
			l.headers[key] = value
		}
	}
}

// WithBasicAuth configures HTTP Basic Authentication for every request made by
// the loader. Transport confidentiality depends on the URL; production callers
// should use HTTPS.
func WithBasicAuth(username, password string) HTTPOption {
	return func(l *HTTPLoader) { l.auth = &BasicAuth{Username: username, Password: password} }
}

// NewHTTP creates a loader that performs an HTTP GET against url. Construction
// does not parse or contact the endpoint. Invalid URLs are reported by Load.
// Options are applied in order, and later options override earlier settings.
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

// Source returns the configured URL without credentials added by
// WithBasicAuth or WithHeaders.
func (l *HTTPLoader) Source() string { return l.url }

// Load performs one GET request and accepts only HTTP 200. It detects the
// response format from Content-Type, then the URL extension, and finally uses
// ParseContent's JSON-then-YAML detection. Transport and status failures wrap
// [confii.ErrConfigLoad]; parsing failures wrap [confii.ErrConfigFormat]. The
// request honors ctx and the configured client timeout.
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
	format := FormatFromContentType(resp.Header.Get("Content-Type"))
	if format == FormatUnknown {
		format = FormatFromExtension(l.url)
	}
	return ParseContent(body, format, l.url)
}

// ParseContent parses data into a configuration map according to format.
//
// Supported formats:
//   - [FormatJSON]: parsed via [encoding/json.Unmarshal].
//   - [FormatYAML]: parsed via [gopkg.in/yaml.v3.Unmarshal].
//   - [FormatTOML]: parsed via [github.com/BurntSushi/toml.Unmarshal], the
//     same implementation used by [TOMLLoader.Load].
//   - [FormatUnknown]: tries JSON first, then YAML. This preserves
//     JSON's stricter interpretation for ambiguous payloads while supporting
//     YAML endpoints that omit both Content-Type and a recognizable extension.
//
// Declared formats are hardened against obvious cross-format input: YAML and
// TOML declarations are validated before decoding, and JSON is decoded only as
// JSON. FormatUnknown does not auto-detect TOML. source is included in the
// returned [confii.ConfigError] and should be stable and non-sensitive.
func ParseContent(data []byte, format Format, source string) (map[string]any, error) {
	var result map[string]any
	var err error

	switch format {
	case FormatJSON:
		err = json.Unmarshal(data, &result)
	case FormatYAML:
		if err = formatparse.ValidateDeclaredContent(formatparse.FormatYAML, data); err == nil {
			err = yaml.Unmarshal(data, &result)
		}
	case FormatTOML:
		if err = formatparse.ValidateDeclaredContent(formatparse.FormatTOML, data); err == nil {
			err = toml.Unmarshal(data, &result)
		}
	case FormatUnknown:
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
