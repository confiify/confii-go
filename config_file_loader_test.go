// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/confiify/confii-go/v2/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileAutoLoader_YAML_NormalizesNonStringKeys(t *testing.T) {
	dir := t.TempDir()

	yamlBody := "ports:\n  80: http\n  443: https\n  ssh: 22\n"
	p := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(yamlBody), 0o644))

	l := &fileAutoLoader{path: p, errorPolicy: ErrorPolicyRaise}
	data, err := l.Load(context.Background())
	require.NoError(t, err)

	ports, ok := data["ports"].(map[string]any)
	require.True(t, ok, "ports must normalize to map[string]any, got %T", data["ports"])

	assert.Equal(t, "http", ports["80"])
	assert.Equal(t, "https", ports["443"])

	assert.EqualValues(t, 22, ports["ssh"])
}

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

func TestFileAutoLoader_INI(t *testing.T) {
	dir := t.TempDir()
	body := "host = localhost\nport = 5432\n\n[database]\nuser = admin\n"
	p := filepath.Join(dir, "config.ini")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))

	l := &fileAutoLoader{path: p, errorPolicy: ErrorPolicyRaise}
	data, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "localhost", data["host"])
	assert.EqualValues(t, 5432, data["port"])
	db, ok := data["database"].(map[string]any)
	require.True(t, ok, "[database] section must surface as nested map")
	assert.Equal(t, "admin", db["user"])
}

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

func TestFileAutoLoader_UnsupportedExtension_Typed(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.xml")

	require.NoError(t, os.WriteFile(p, []byte("<root><key>value</key></root>\n"), 0o644))

	l := &fileAutoLoader{path: p, errorPolicy: ErrorPolicyRaise}
	_, err := l.Load(context.Background())
	require.Error(t, err)
	var ce *ConfigError
	require.True(t, errors.As(err, &ce), "expected *ConfigError, got %T", err)
	assert.True(t, errors.Is(err, ErrConfigFormat),
		"unsupported extension must wrap ErrConfigFormat")
	assert.Contains(t, err.Error(), "unsupported declarative file source")
	assert.Contains(t, err.Error(), p)
}

func TestFileAutoLoader_ErrorPolicy_Warn_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")

	require.NoError(t, os.WriteFile(bad, []byte(":\n\t- not: valid\n"), 0o644))

	l := &fileAutoLoader{
		path:        bad,
		errorPolicy: ErrorPolicyWarn,
		logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	_, err := l.Load(context.Background())
	require.Error(t, err, "malformed YAML must surface a typed parse error")
	var ce *ConfigError
	require.True(t, errors.As(err, &ce))
	assert.True(t, errors.Is(err, ErrConfigFormat))
}

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

func TestApplySelfConfig_TOMLSource_EndToEnd(t *testing.T) {
	dir := t.TempDir()

	confiiYAML := "sources:\n  - type: toml\n    path: config.toml\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte(confiiYAML), 0o644))

	tomlBody := "key = \"from-toml\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte(tomlBody), 0o644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	defer func() { _ = os.Chdir(origWD) }()
	selfconfig.ClearCache()
	defer selfconfig.ClearCache()

	cfg, err := NewWithContext[any](context.Background())
	require.NoError(t, err)
	v, err := cfg.Get("key")
	require.NoError(t, err)
	assert.Equal(t, "from-toml", v)
}

func TestRollback_OnFileAutoLoaderSchemaViolation(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	require.NoError(t, os.WriteFile(cfgPath, []byte("port: 80\n"), 0o644))

	schemaPath := writeSchemaFile(t, g01ValidSchema)

	l := &fileAutoLoader{path: cfgPath, errorPolicy: ErrorPolicyRaise}
	_, err := NewWithContext[any](context.Background(),
		WithLoaders(l),
		WithSchemaPath(schemaPath),
		WithValidateOnLoad(true),
	)
	require.Error(t, err, "schema violation must fail New")
	assert.True(t, errors.Is(err, ErrConfigValidation))

	require.NoError(t, os.WriteFile(cfgPath, []byte("port: 5432\n"), 0o644))
	cfg, err := NewWithContext[any](context.Background(),
		WithLoaders(&fileAutoLoader{path: cfgPath, errorPolicy: ErrorPolicyRaise}),
		WithSchemaPath(schemaPath),
		WithValidateOnLoad(true),
	)
	require.NoError(t, err)
	v1, _ := cfg.Get("port")
	assert.EqualValues(t, 5432, v1)

	require.NoError(t, os.WriteFile(cfgPath, []byte("port: 80\n"), 0o644))
	err = cfg.ReloadWithContext(context.Background(), WithIncremental(false))
	require.Error(t, err, "Reload must fail when new state violates schema")
	assert.True(t, errors.Is(err, ErrConfigValidation))

	v2, _ := cfg.Get("port")
	assert.EqualValues(t, 5432, v2,
		"a failed reload must restore the prior state")
}
