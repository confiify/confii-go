package compose

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// _defaults with file references (string, not inline map)
// ---------------------------------------------------------------------------

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
	// If _defaults is something unexpected (e.g., an int), it should be ignored.
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
				"optional": true, // should be skipped
			},
		},
		"database": "mysql", // overrides default
	}

	c := New(".")
	result, err := c.Compose(config, "test.yaml")
	require.NoError(t, err)
	assert.Equal(t, "mysql", result["database"]) // current value wins over default
	assert.Equal(t, "redis", result["cache"])    // from defaults
}

// ---------------------------------------------------------------------------
// _include with single string (not array)
// ---------------------------------------------------------------------------

func TestCompose_Include_SingleString(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "extra.yaml"), []byte("extra_key: extra_val\n"), 0644)

	config := map[string]any{
		"_include": "extra.yaml",
		"main":     "value",
	}

	c := New(dir)
	result, err := c.Compose(config, filepath.Join(dir, "main.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "value", result["main"])
	assert.Equal(t, "extra_val", result["extra_key"])
}

// ---------------------------------------------------------------------------
// _include with non-string/non-slice value (ignored)
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// _include with mixed array (string and non-string items)
// ---------------------------------------------------------------------------

func TestCompose_Include_MixedArray(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "valid.yaml"), []byte("key: val\n"), 0644)

	config := map[string]any{
		"_include": []any{"valid.yaml", 42, true}, // non-string items should be skipped
	}

	c := New(dir)
	result, err := c.Compose(config, filepath.Join(dir, "main.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "val", result["key"])
}

// ---------------------------------------------------------------------------
// _include with file not found
// ---------------------------------------------------------------------------

func TestCompose_Include_FileNotFound(t *testing.T) {
	config := map[string]any{
		"_include": []any{"nonexistent.yaml"},
	}

	c := New(".")
	_, err := c.Compose(config, "test.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "include")
}

// ---------------------------------------------------------------------------
// _include with absolute path
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// loadFile with JSON format
// ---------------------------------------------------------------------------

func TestCompose_Include_JSONFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "extra.json"), []byte(`{"json_key": "json_val"}`), 0644)

	config := map[string]any{
		"_include": []any{"extra.json"},
	}

	c := New(dir)
	result, err := c.Compose(config, filepath.Join(dir, "main.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "json_val", result["json_key"])
}

// ---------------------------------------------------------------------------
// loadFile with TOML format
// ---------------------------------------------------------------------------

func TestCompose_Include_TOMLFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "extra.toml"), []byte("toml_key = \"toml_val\"\n"), 0644)

	config := map[string]any{
		"_include": []any{"extra.toml"},
	}

	c := New(dir)
	result, err := c.Compose(config, filepath.Join(dir, "main.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "toml_val", result["toml_key"])
}

// ---------------------------------------------------------------------------
// loadFile with unknown extension (defaults to YAML)
// ---------------------------------------------------------------------------

func TestCompose_Include_UnknownExtension(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "extra.cfg"), []byte("cfg_key: cfg_val\n"), 0644)

	config := map[string]any{
		"_include": []any{"extra.cfg"},
	}

	c := New(dir)
	result, err := c.Compose(config, filepath.Join(dir, "main.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "cfg_val", result["cfg_key"])
}

// ---------------------------------------------------------------------------
// loadFile with parse error
// ---------------------------------------------------------------------------

func TestCompose_Include_ParseError(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{invalid json"), 0644)

	config := map[string]any{
		"_include": []any{"bad.json"},
	}

	c := New(dir)
	_, err := c.Compose(config, filepath.Join(dir, "main.yaml"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

// ---------------------------------------------------------------------------
// New with empty basePath defaults to "."
// ---------------------------------------------------------------------------

func TestNew_EmptyBasePath(t *testing.T) {
	c := New("")
	assert.Equal(t, ".", c.basePath)
}

// ---------------------------------------------------------------------------
// Compose with no directives (passthrough)
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Recursive composition in included files
// ---------------------------------------------------------------------------

func TestCompose_RecursiveIncludes(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "level1.yaml"), []byte("_include:\n  - level2.yaml\nlevel1: true\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "level2.yaml"), []byte("level2: true\n"), 0644)

	config := map[string]any{
		"_include": []any{"level1.yaml"},
	}

	c := New(dir)
	result, err := c.Compose(config, filepath.Join(dir, "main.yaml"))
	require.NoError(t, err)
	assert.Equal(t, true, result["level1"])
	assert.Equal(t, true, result["level2"])
}

// ---------------------------------------------------------------------------
// Both _defaults and _include together
// ---------------------------------------------------------------------------

func TestCompose_DefaultsAndIncludes(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "inc.yaml"), []byte("from_include: included\n"), 0644)

	config := map[string]any{
		"_defaults": []any{"default_key: default_val"},
		"_include":  []any{"inc.yaml"},
		"own_key":   "own_val",
	}

	c := New(dir)
	result, err := c.Compose(config, filepath.Join(dir, "main.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "own_val", result["own_key"])
	assert.Equal(t, "included", result["from_include"])
	assert.Equal(t, "default_val", result["default_key"])
}

// ---------------------------------------------------------------------------
// _defaults string entry without colon (skipped)
// ---------------------------------------------------------------------------

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
	// "no_colon_here" has no ":" so it produces an entry with empty key via Cut returning false.
	// Actually, strings.Cut on a string without ":" returns (original, "", false), so ok is false
	// and the entry is skipped.
	_, has := result["no_colon_here"]
	assert.False(t, has)
}

// ---------------------------------------------------------------------------
// Source empty dir falls back to basePath
// ---------------------------------------------------------------------------

// ===========================================================================
// _defaults pointing to a non-existent file to trigger processDefaults error path
// ===========================================================================

func TestCompose_Defaults_NonExistentFile(t *testing.T) {
	// The processDefaults function handles string items as "key: value" format,
	// so pointing to a non-existent file path doesn't error because it's treated
	// as a string key-value. The _include directive would error for missing files.
	// Test _include pointing to nonexistent file to trigger error path.
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

	// Source is just a filename with no directory.
	c := New(dir)
	result, err := c.Compose(config, "main.yaml")
	require.NoError(t, err)
	assert.Equal(t, true, result["inc"])
}
