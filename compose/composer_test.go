package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompose_Include(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "base.yaml"), []byte("database:\n  host: localhost\n  port: 5432\n"), 0644)

	config := map[string]any{
		"_include": []any{"base.yaml"},
		"app":      map[string]any{"name": "myapp"},
	}

	c := New(dir)
	result, err := c.Compose(config, filepath.Join(dir, "main.yaml"))
	require.NoError(t, err)

	_, hasInclude := result["_include"]
	assert.False(t, hasInclude)

	db, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "localhost", db["host"])

	app := result["app"].(map[string]any)
	assert.Equal(t, "myapp", app["name"])
}

func TestCompose_Defaults(t *testing.T) {
	config := map[string]any{
		"_defaults": []any{
			"database: postgres",
			map[string]any{"cache": "redis", "optional": true},
		},
		"app": "myapp",
	}

	c := New(".")
	result, err := c.Compose(config, "test.yaml")
	require.NoError(t, err)

	_, hasDefaults := result["_defaults"]
	assert.False(t, hasDefaults)

	assert.Equal(t, "postgres", result["database"])
	assert.Equal(t, "redis", result["cache"])
	assert.Equal(t, "myapp", result["app"])
}

func TestCompose_CycleDetection(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "a.yaml"), []byte("_include:\n  - b.yaml\nfrom_a: true\n"), 0644)
	os.WriteFile(filepath.Join(dir, "b.yaml"), []byte("_include:\n  - a.yaml\nfrom_b: true\n"), 0644)

	config := map[string]any{
		"_include": []any{"a.yaml"},
	}

	c := New(dir)
	result, err := c.Compose(config, filepath.Join(dir, "main.yaml"))
	require.NoError(t, err)

	assert.Equal(t, true, result["from_a"])
	assert.Equal(t, true, result["from_b"])
}

func TestCompose_MaxDepth(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < maxDepth+2; i++ {
		next := fmt.Sprintf("level%d.yaml", i+1)
		content := fmt.Sprintf("_include:\n  - %s\nlevel%d: true\n", next, i)
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("level%d.yaml", i)), []byte(content), 0644)
	}
	os.WriteFile(filepath.Join(dir, fmt.Sprintf("level%d.yaml", maxDepth+2)), []byte("leaf: true\n"), 0644)

	config := map[string]any{"_include": []any{"level0.yaml"}}
	c := New(dir)
	_, err := c.Compose(config, filepath.Join(dir, "main.yaml"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max depth")
}

func TestCompose_MergeStrategyRemoved(t *testing.T) {
	config := map[string]any{
		"_merge_strategy": "replace",
		"key":             "value",
	}

	c := New(".")
	result, err := c.Compose(config, "test.yaml")
	require.NoError(t, err)

	_, hasMergeStrategy := result["_merge_strategy"]
	assert.False(t, hasMergeStrategy)
	assert.Equal(t, "value", result["key"])
}
