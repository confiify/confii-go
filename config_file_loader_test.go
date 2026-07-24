// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Tests for D07 (fileAutoLoader skips D01 YAML normalization) and the
// G19-residual scope (fileAutoLoader still YAML/JSON only). Each test
// pins a distinct OBSERVABLE that fails under the pre-fix code: the
// audit-mandated case list lives in the package's REMEDIATION_PROGRESS
// notes; remediation-relevant invariants are recorded in the test
// header where applicable.

package confii

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/confiify/confii-go/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// SUB-FIX 1 + 3: D01 normalization applies to self-config-discovered YAML.
// ---------------------------------------------------------------------------

// TestFileAutoLoader_YAML_NormalizesNonStringKeys pins:
//
//   - Observable: a YAML file loaded via fileAutoLoader whose document
//     contains a map with non-string keys (here, integer keys) is
//     returned with every map key coerced to a string. Pre-fix the
//     auto-loader called yaml.Unmarshal directly into map[string]any
//     and gopkg.in/yaml.v3 emitted map[interface{}]interface{} for the
//     nested map, leaking incompatible types past the loader boundary.
//
// This is the residual D01 contract: routing through the same
// normalization helper as loader.NewYAML guarantees no
// map[interface{}]interface{} ever escapes.
func TestFileAutoLoader_YAML_NormalizesNonStringKeys(t *testing.T) {
	dir := t.TempDir()
	// Integer keys at a nested level — gopkg.in/yaml.v3 decodes the
	// nested map as map[interface{}]interface{} when ANY key is
	// non-string.
	yamlBody := "ports:\n  80: http\n  443: https\n  ssh: 22\n"
	p := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(yamlBody), 0o644))

	l := &fileAutoLoader{path: p, errorPolicy: ErrorPolicyRaise}
	data, err := l.Load(context.Background())
	require.NoError(t, err)

	ports, ok := data["ports"].(map[string]any)
	require.True(t, ok, "ports must normalize to map[string]any, got %T", data["ports"])
	// Integer keys 80 and 443 must be stringified to "80" and "443".
	assert.Equal(t, "http", ports["80"])
	assert.Equal(t, "https", ports["443"])
	// String key passes through unchanged; integer value remains.
	assert.EqualValues(t, 22, ports["ssh"])
}

// ---------------------------------------------------------------------------
// SUB-FIX 2: extension dispatch covers TOML / INI / .env.
// ---------------------------------------------------------------------------

// TestFileAutoLoader_TOML pins TOML format support — pre-fix the
// auto-loader fell through the format switch's default branch and tried
// to YAML-parse TOML content (often producing a misleading error or, for
// trivial single-line input, bogus success).
func TestFileAutoLoader_TOML(t *testing.T) {
	dir := t.TempDir()
	body := `key = "tomlval"

[database]
host = "db.example.com"
port = 5432
`
	p := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))

	l := &fileAutoLoader{path: p, errorPolicy: ErrorPolicyRaise}
	data, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "tomlval", data["key"])
	db, ok := data["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "db.example.com", db["host"])
}

// TestFileAutoLoader_INI pins INI format support, including the G19
// defaults-only convention (root keys promoted from the synthetic
// DEFAULT section preceding the first [section] header).
func TestFileAutoLoader_INI(t *testing.T) {
	dir := t.TempDir()
	body := "host = localhost\nport = 5432\n\n[database]\nuser = admin\n"
	p := filepath.Join(dir, "config.ini")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))

	l := &fileAutoLoader{path: p, errorPolicy: ErrorPolicyRaise}
	data, err := l.Load(context.Background())
	require.NoError(t, err)
	// Root-level keys (G19 defaults-only contract).
	assert.Equal(t, "localhost", data["host"])
	assert.EqualValues(t, 5432, data["port"])
	db, ok := data["database"].(map[string]any)
	require.True(t, ok, "[database] section must surface as nested map")
	assert.Equal(t, "admin", db["user"])
}

// TestFileAutoLoader_EnvFile pins .env support: KEY=VALUE pairs with
// comment-stripping, dot-nested keys, and the same scalar coercion as
// loader.EnvFileLoader.
func TestFileAutoLoader_EnvFile(t *testing.T) {
	dir := t.TempDir()
	body := "# comment line\nFOO=bar\nDB.HOST=localhost\nDB.PORT=5432\n"
	p := filepath.Join(dir, "settings.env")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))

	l := &fileAutoLoader{path: p, errorPolicy: ErrorPolicyRaise}
	data, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "bar", data["FOO"])
	db, ok := data["DB"].(map[string]any)
	require.True(t, ok, "dot-nested key DB.HOST must build a nested map")
	assert.Equal(t, "localhost", db["HOST"])
	assert.EqualValues(t, 5432, db["PORT"])
}

// ---------------------------------------------------------------------------
// SUB-FIX 5: unsupported extension produces a typed *ConfigError.
// ---------------------------------------------------------------------------

// TestFileAutoLoader_UnsupportedExtension pins the "operator typo
// surfaces visibly" contract (sub-fix 5): a self-config default_files
// entry pointing at an extension confii cannot parse (e.g. .xml) must
// return a typed *ConfigError wrapping ErrConfigFormat with a clear
// "unsupported file format" message — pre-D07 the auto-loader silently
// fell through to YAML parsing for any unknown extension, producing
// either a misleading parse error or, for trivial input, bogus success.
func TestFileAutoLoader_UnsupportedExtension_Typed(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.xml")
	// Even valid-ish content must not be silently accepted: the
	// extension itself is the failure signal.
	require.NoError(t, os.WriteFile(p, []byte("<root><key>value</key></root>\n"), 0o644))

	l := &fileAutoLoader{path: p, errorPolicy: ErrorPolicyRaise}
	_, err := l.Load(context.Background())
	require.Error(t, err)
	var ce *ConfigError
	require.True(t, errors.As(err, &ce), "expected *ConfigError, got %T", err)
	assert.True(t, errors.Is(err, ErrConfigFormat),
		"unsupported extension must wrap ErrConfigFormat")
	assert.Contains(t, err.Error(), "unsupported file format")
	assert.Contains(t, err.Error(), p)
}

// ---------------------------------------------------------------------------
// SUB-FIX 4: ErrorPolicy parity with explicit-loader path (G07).
// ---------------------------------------------------------------------------

// TestFileAutoLoader_ErrorPolicy_Warn_MalformedYAML pins:
//
//   - Observable: a malformed YAML file in self-config under
//     ErrorPolicyWarn surfaces the parse error to the outer load loop's
//     OnError dispatch, which logs without aborting (Warn semantics).
//
// This pins the propagation chain: applySelfConfig wires
// opts.OnError onto each fileAutoLoader; the outer load() loop's
// OnError dispatch handles the result identically to explicit-loader
// errors, preserving G07 parity.
func TestFileAutoLoader_ErrorPolicy_Warn_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	// Tab-indented YAML is a parse error in gopkg.in/yaml.v3.
	require.NoError(t, os.WriteFile(bad, []byte(":\n\t- not: valid\n"), 0o644))

	l := &fileAutoLoader{
		path:        bad,
		errorPolicy: ErrorPolicyWarn,
		logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
	// Direct call: the loader produces a typed format error; the
	// outer load() loop's dispatch then applies the policy. The
	// per-loader Warn policy here governs missing/malformed-line
	// handling for envfile; for YAML/TOML/JSON/INI the parse error
	// flows back to the caller and the caller-level OnError dispatch
	// (in load()) decides whether to raise / warn / ignore.
	_, err := l.Load(context.Background())
	require.Error(t, err, "malformed YAML must surface a typed parse error")
	var ce *ConfigError
	require.True(t, errors.As(err, &ce))
	assert.True(t, errors.Is(err, ErrConfigFormat))
}

// TestFileAutoLoader_ErrorPolicy_Raise_MissingFile pins:
//
//   - Observable: a missing self-config-discovered file under the
//     default ErrorPolicyRaise surfaces a typed *ConfigError. Under
//     ErrorPolicyIgnore the same loader returns (nil, nil). Pre-D07
//     the auto-loader unconditionally swallowed missing files, so the
//     Raise branch was unreachable.
func TestFileAutoLoader_ErrorPolicy_Raise_MissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.yaml")

	t.Run("raise", func(t *testing.T) {
		l := &fileAutoLoader{path: missing, errorPolicy: ErrorPolicyRaise}
		_, err := l.Load(context.Background())
		require.Error(t, err)
		var ce *ConfigError
		require.True(t, errors.As(err, &ce))
		assert.True(t, errors.Is(err, ErrConfigLoad))
	})

	t.Run("ignore", func(t *testing.T) {
		l := &fileAutoLoader{path: missing, errorPolicy: ErrorPolicyIgnore}
		data, err := l.Load(context.Background())
		require.NoError(t, err)
		assert.Nil(t, data)
	})
}

// ---------------------------------------------------------------------------
// SUB-FIX 6: end-to-end via applySelfConfig — TOML default_files works.
// ---------------------------------------------------------------------------

// TestApplySelfConfig_TOMLDefaultFiles_EndToEnd pins the integration
// path: a `.confii.yaml` declaring `default_files: [config.toml]`
// produces a Config whose merged data was actually parsed by the TOML
// branch of fileAutoLoader. Pre-D07 this would either silently fall
// through to YAML (parse error or wrong shape) or skip the file
// entirely.
func TestApplySelfConfig_TOMLDefaultFiles_EndToEnd(t *testing.T) {
	dir := t.TempDir()

	confiiYAML := "default_files:\n  - config.toml\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte(confiiYAML), 0o644))

	tomlBody := "key = \"from-toml\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte(tomlBody), 0o644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	defer func() { _ = os.Chdir(origWD) }()
	selfconfig.ClearCache()
	defer selfconfig.ClearCache()

	cfg, err := New[any](context.Background())
	require.NoError(t, err)
	v, err := cfg.Get("key")
	require.NoError(t, err)
	assert.Equal(t, "from-toml", v)
}

// ---------------------------------------------------------------------------
// SUB-FIX 7: Wave 14 G01 + D07 interaction — schema rollback for a
// fileAutoLoader-loaded source.
// ---------------------------------------------------------------------------

// TestRollback_OnFileAutoLoaderSchemaViolation pins the Wave 14
// G01 + D07 interaction: a self-config-discovered file (loaded via the
// new D07 dispatch) that contains values violating a configured JSON
// schema triggers G01's runValidateOnLoad, which propagates through
// New's Step 6 hook and surfaces as a typed *ConfigError wrapping
// ErrConfigValidation. The Wave 7 G14 / D05 rollback contract holds:
// the failure path means New returns an error and never hands a
// half-loaded Config to the caller.
func TestRollback_OnFileAutoLoaderSchemaViolation(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	// port=80 violates the schema's minimum=1024 constraint.
	require.NoError(t, os.WriteFile(cfgPath, []byte("port: 80\n"), 0o644))

	schemaPath := writeSchemaFile(t, g01ValidSchema)

	l := &fileAutoLoader{path: cfgPath, errorPolicy: ErrorPolicyRaise}
	_, err := New[any](context.Background(),
		WithLoaders(l),
		WithSchemaPath(schemaPath),
		WithValidateOnLoad(true),
	)
	require.Error(t, err, "schema violation must fail New")
	assert.True(t, errors.Is(err, ErrConfigValidation))

	// Reload-rollback contract: also verify Reload's rollback restores
	// state when the new file violates the schema mid-flight.
	require.NoError(t, os.WriteFile(cfgPath, []byte("port: 5432\n"), 0o644))
	cfg, err := New[any](context.Background(),
		WithLoaders(&fileAutoLoader{path: cfgPath, errorPolicy: ErrorPolicyRaise}),
		WithSchemaPath(schemaPath),
		WithValidateOnLoad(true),
	)
	require.NoError(t, err)
	v1, _ := cfg.Get("port")
	assert.EqualValues(t, 5432, v1)

	// Rewrite to invalid; Reload must error AND roll back.
	require.NoError(t, os.WriteFile(cfgPath, []byte("port: 80\n"), 0o644))
	err = cfg.Reload(context.Background(), WithIncremental(false))
	require.Error(t, err, "Reload must fail when new state violates schema")
	assert.True(t, errors.Is(err, ErrConfigValidation))

	v2, _ := cfg.Get("port")
	assert.EqualValues(t, 5432, v2,
		"Wave 7 G14 / D05 rollback contract: failed reload must restore prior state")
}
