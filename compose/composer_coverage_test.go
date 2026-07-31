// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package compose

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompose_Defaults_SingleString(t *testing.T) {
	config := map[string]any{
		"_defaults": "cache: memcached",
		"app":       "myapp",
	}

	c := New(".")
	result, err := c.Compose(config, "test.yaml")
	require.NoError(t, err)
	assert.Equal(t, "memcached", result["cache"])
	assert.Equal(t, "myapp", result["app"])
}

func TestCompose_Defaults_NonStringNonSlice(t *testing.T) {

	config := map[string]any{
		"_defaults": 42,
		"app":       "myapp",
	}

	c := New(".")
	result, err := c.Compose(config, "test.yaml")
	require.NoError(t, err)
	assert.Equal(t, "myapp", result["app"])
	_, hasDefaults := result["_defaults"]
	assert.False(t, hasDefaults)
}

func TestCompose_Defaults_InlineMapValues(t *testing.T) {
	config := map[string]any{
		"_defaults": []any{
			map[string]any{
				"database": "postgres",
				"cache":    "redis",
				"optional": true,
			},
		},
		"database": "mysql",
	}

	c := New(".")
	result, err := c.Compose(config, "test.yaml")
	require.NoError(t, err)
	assert.Equal(t, "mysql", result["database"])
	assert.Equal(t, "redis", result["cache"])
}

func TestCompose_Include_SingleString(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "extra.yaml"), []byte("extra_key: extra_val\n"), 0644)

	config := map[string]any{
		"_include": "extra.yaml",
		"main":     "value",
	}

	c := New(dir)
	result, err := c.Compose(config, filepath.Join(dir, "main().yaml"))
	require.NoError(t, err)
	assert.Equal(t, "value", result["main"])
	assert.Equal(t, "extra_val", result["extra_key"])
}

func TestCompose_Include_NonStringNonSlice(t *testing.T) {
	config := map[string]any{
		"_include": 42,
		"app":      "myapp",
	}

	c := New(".")
	result, err := c.Compose(config, "test.yaml")
	require.NoError(t, err)
	assert.Equal(t, "myapp", result["app"])
	_, hasInclude := result["_include"]
	assert.False(t, hasInclude)
}

func TestCompose_Include_MixedArray(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "valid.yaml"), []byte("key: val\n"), 0644)

	config := map[string]any{
		"_include": []any{"valid.yaml", 42, true},
	}

	c := New(dir)
	result, err := c.Compose(config, filepath.Join(dir, "main().yaml"))
	require.NoError(t, err)
	assert.Equal(t, "val", result["key"])
}

func TestCompose_Include_FileNotFound(t *testing.T) {
	config := map[string]any{
		"_include": []any{"nonexistent.yaml"},
	}

	c := New(".")
	_, err := c.Compose(config, "test.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "include")
}

func TestCompose_Include_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	absPath := filepath.Join(dir, "absolute.yaml")
	_ = os.WriteFile(absPath, []byte("from_abs: true\n"), 0644)

	config := map[string]any{
		"_include": []any{absPath},
	}

	c := New(".")
	result, err := c.Compose(config, "test.yaml")
	require.NoError(t, err)
	assert.Equal(t, true, result["from_abs"])
}

func TestCompose_Include_JSONFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "extra.json"), []byte(`{"json_key": "json_val"}`), 0644)

	config := map[string]any{
		"_include": []any{"extra.json"},
	}

	c := New(dir)
	result, err := c.Compose(config, filepath.Join(dir, "main().yaml"))
	require.NoError(t, err)
	assert.Equal(t, "json_val", result["json_key"])
}

func TestCompose_Include_TOMLFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "extra.toml"), []byte("toml_key = \"toml_val\"\n"), 0644)

	config := map[string]any{
		"_include": []any{"extra.toml"},
	}

	c := New(dir)
	result, err := c.Compose(config, filepath.Join(dir, "main().yaml"))
	require.NoError(t, err)
	assert.Equal(t, "toml_val", result["toml_key"])
}

func TestCompose_Include_UnknownExtension(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "extra.cfg"), []byte("cfg_key: cfg_val\n"), 0644)

	config := map[string]any{
		"_include": []any{"extra.cfg"},
	}

	c := New(dir)
	_, err := c.Compose(config, filepath.Join(dir, "main().yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported configuration extension")
}

func TestCompose_Include_YAMLRejectsJSONDocument(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.yaml"), []byte(`{"json_key":"json_val"}`), 0o600))

	c := New(dir)
	_, err := c.Compose(map[string]any{"_include": []any{"extra.yaml"}}, filepath.Join(dir, "main().yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JSON document")
}

func TestCompose_Include_ParseError(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{invalid json"), 0644)

	config := map[string]any{
		"_include": []any{"bad.json"},
	}

	c := New(dir)
	_, err := c.Compose(config, filepath.Join(dir, "main().yaml"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

func TestNew_EmptyBasePath(t *testing.T) {
	c := New("")
	assert.Equal(t, ".", c.basePath)
}

func TestCompose_NoDirectives(t *testing.T) {
	config := map[string]any{
		"key1": "value1",
		"key2": map[string]any{"nested": "value2"},
	}

	c := New(".")
	result, err := c.Compose(config, "test.yaml")
	require.NoError(t, err)
	assert.Equal(t, "value1", result["key1"])
	nested := result["key2"].(map[string]any)
	assert.Equal(t, "value2", nested["nested"])
}

func TestCompose_RecursiveIncludes(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "level1.yaml"), []byte("_include:\n  - level2.yaml\nlevel1: true\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "level2.yaml"), []byte("level2: true\n"), 0644)

	config := map[string]any{
		"_include": []any{"level1.yaml"},
	}

	c := New(dir)
	result, err := c.Compose(config, filepath.Join(dir, "main().yaml"))
	require.NoError(t, err)
	assert.Equal(t, true, result["level1"])
	assert.Equal(t, true, result["level2"])
}

func TestCompose_DefaultsAndIncludes(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "inc.yaml"), []byte("from_include: included\n"), 0644)

	config := map[string]any{
		"_defaults": []any{"default_key: default_val"},
		"_include":  []any{"inc.yaml"},
		"own_key":   "own_val",
	}

	c := New(dir)
	result, err := c.Compose(config, filepath.Join(dir, "main().yaml"))
	require.NoError(t, err)
	assert.Equal(t, "own_val", result["own_key"])
	assert.Equal(t, "included", result["from_include"])
	assert.Equal(t, "default_val", result["default_key"])
}

func TestCompose_Defaults_StringWithoutColon(t *testing.T) {
	config := map[string]any{
		"_defaults": []any{
			"valid_key: valid_value",
			"no_colon_here",
		},
	}

	c := New(".")
	result, err := c.Compose(config, "test.yaml")
	require.NoError(t, err)
	assert.Equal(t, "valid_value", result["valid_key"])

	_, has := result["no_colon_here"]
	assert.False(t, has)
}

func TestCompose_Defaults_NonExistentFile(t *testing.T) {

	config := map[string]any{
		"_include": []any{"nonexistent_defaults_file.yaml"},
		"key":      "value",
	}

	c := New(".")
	_, err := c.Compose(config, "test.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "include")
}

func TestCompose_Include_EmptySourceDir(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "inc.yaml"), []byte("inc: true\n"), 0644)

	config := map[string]any{
		"_include": []any{"inc.yaml"},
	}

	c := New(dir)
	result, err := c.Compose(config, "main().yaml")
	require.NoError(t, err)
	assert.Equal(t, true, result["inc"])
}

func TestNew_RestoresDefaultMergerAfterOptionClearsIt(t *testing.T) {
	c := New("", func(c *Composer) { c.merger = nil })
	got, err := c.Compose(map[string]any{
		"_defaults": []any{map[string]any{"from_default": true}},
		"explicit":  true,
	}, "config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got["from_default"] != true || got["explicit"] != true {
		t.Fatalf("default merger result = %#v", got)
	}
}
