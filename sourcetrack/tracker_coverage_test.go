package sourcetrack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTracker(t *testing.T) {
	tr := NewTracker(false)
	require.NotNil(t, tr)
	assert.NotNil(t, tr.sources)
	assert.False(t, tr.debugMode)

	trDebug := NewTracker(true)
	assert.True(t, trDebug.debugMode)
}

func TestTrackValue_Single(t *testing.T) {
	tr := NewTracker(false)
	tr.TrackValue("app.port", 8080, "config.yaml", "yaml", "dev")

	info := tr.GetSourceInfo("app.port")
	require.NotNil(t, info)
	assert.Equal(t, "app.port", info.Key)
	assert.Equal(t, 8080, info.Value)
	assert.Equal(t, "config.yaml", info.SourceFile)
	assert.Equal(t, "yaml", info.LoaderType)
	assert.Equal(t, "dev", info.Environment)
	assert.Equal(t, 0, info.OverrideCount)
	assert.False(t, info.Timestamp.IsZero())
}

func TestTrackValue_Override(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("app.port", 8080, "config.yaml", "yaml", "dev")
	tr.TrackValue("app.port", 9090, "override.yaml", "yaml", "prod")

	info := tr.GetSourceInfo("app.port")
	require.NotNil(t, info)
	assert.Equal(t, 9090, info.Value)
	assert.Equal(t, "override.yaml", info.SourceFile)
	assert.Equal(t, 1, info.OverrideCount)

	// In debug mode, history should be recorded.
	require.Len(t, info.History, 1)
	assert.Equal(t, 8080, info.History[0].Value)
	assert.Equal(t, "config.yaml", info.History[0].Source)
}

func TestTrackValue_OverrideNonDebug(t *testing.T) {
	tr := NewTracker(false)
	tr.TrackValue("key", "v1", "a.yaml", "yaml", "")
	tr.TrackValue("key", "v2", "b.yaml", "yaml", "")

	info := tr.GetSourceInfo("key")
	require.NotNil(t, info)
	assert.Equal(t, 1, info.OverrideCount)
	// No history in non-debug mode.
	assert.Empty(t, info.History)
}

func TestTrackConfig(t *testing.T) {
	tr := NewTracker(false)
	config := map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 5432,
		},
		"debug": true,
	}
	tr.TrackConfig(config, "app.yaml", "yaml", "dev", "")

	info := tr.GetSourceInfo("database.host")
	require.NotNil(t, info)
	assert.Equal(t, "localhost", info.Value)

	info = tr.GetSourceInfo("database.port")
	require.NotNil(t, info)
	assert.Equal(t, 5432, info.Value)

	info = tr.GetSourceInfo("debug")
	require.NotNil(t, info)
	assert.Equal(t, true, info.Value)
}

func TestTrackConfig_WithPrefix(t *testing.T) {
	tr := NewTracker(false)
	config := map[string]any{"name": "myapp"}
	tr.TrackConfig(config, "app.yaml", "yaml", "", "app")

	info := tr.GetSourceInfo("app.name")
	require.NotNil(t, info)
	assert.Equal(t, "myapp", info.Value)
}

func TestGetSourceInfo_Missing(t *testing.T) {
	tr := NewTracker(false)
	assert.Nil(t, tr.GetSourceInfo("nonexistent"))
}

func TestGetOverrideHistory_WithHistory(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("k", "a", "s1", "yaml", "")
	tr.TrackValue("k", "b", "s2", "env", "")
	tr.TrackValue("k", "c", "s3", "json", "")

	hist := tr.GetOverrideHistory("k")
	require.Len(t, hist, 2)
	assert.Equal(t, "a", hist[0].Value)
	assert.Equal(t, "b", hist[1].Value)
}

func TestGetOverrideHistory_NoHistory(t *testing.T) {
	tr := NewTracker(false)
	assert.Nil(t, tr.GetOverrideHistory("missing"))

	tr.TrackValue("k", "v", "s", "yaml", "")
	hist := tr.GetOverrideHistory("k")
	assert.Empty(t, hist)
}

func TestGetConflicts(t *testing.T) {
	tr := NewTracker(false)
	tr.TrackValue("a", 1, "s1", "yaml", "")
	tr.TrackValue("b", 2, "s1", "yaml", "")
	tr.TrackValue("a", 10, "s2", "env", "")

	conflicts := tr.GetConflicts()
	assert.Len(t, conflicts, 1)
	assert.Contains(t, conflicts, "a")
	assert.Equal(t, 1, conflicts["a"].OverrideCount)
}

func TestGetConflicts_None(t *testing.T) {
	tr := NewTracker(false)
	tr.TrackValue("a", 1, "s1", "yaml", "")
	assert.Empty(t, tr.GetConflicts())
}

func TestFindKeysFromSource_ExactMatch(t *testing.T) {
	tr := NewTracker(false)
	tr.TrackValue("a", 1, "config.yaml", "yaml", "")
	tr.TrackValue("b", 2, "secrets.env", "env", "")

	keys := tr.FindKeysFromSource("config.yaml")
	assert.Equal(t, []string{"a"}, keys)
}

func TestFindKeysFromSource_PatternMatch(t *testing.T) {
	tr := NewTracker(false)
	tr.TrackValue("x", 1, "/etc/config.yaml", "yaml", "")
	tr.TrackValue("y", 2, "/etc/config.json", "json", "")
	tr.TrackValue("z", 3, "/home/secrets.env", "env", "")

	keys := tr.FindKeysFromSource("/etc/")
	assert.Len(t, keys, 2)
	assert.Contains(t, keys, "x")
	assert.Contains(t, keys, "y")
}

func TestGetSourceStatistics(t *testing.T) {
	tr := NewTracker(false)
	tr.TrackValue("a", 1, "config.yaml", "yaml", "")
	tr.TrackValue("b", 2, "config.yaml", "yaml", "")
	tr.TrackValue("c", 3, "secrets.env", "env", "")
	// Override a to add an override count.
	tr.TrackValue("a", 10, "secrets.env", "env", "")

	stats := tr.GetSourceStatistics()
	assert.Equal(t, 3, stats["total_keys"])
	assert.Equal(t, 1, stats["total_overrides"])

	sources, ok := stats["sources"].(map[string]int)
	require.True(t, ok)
	// After override, "a" is now from secrets.env.
	assert.Equal(t, 1, sources["config.yaml"])
	assert.Equal(t, 2, sources["secrets.env"])

	loaders, ok := stats["loader_types"].(map[string]int)
	require.True(t, ok)
	assert.Equal(t, 1, loaders["yaml"])
	assert.Equal(t, 2, loaders["env"])
}

func TestExportDebugReport(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("app.port", 8080, "config.yaml", "yaml", "dev")

	dir := t.TempDir()
	outPath := filepath.Join(dir, "report.json")

	err := tr.ExportDebugReport(outPath)
	require.NoError(t, err)

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)

	var report map[string]any
	err = json.Unmarshal(data, &report)
	require.NoError(t, err)
	assert.Contains(t, report, "app.port")
}

func TestPrintDebugInfo_SpecificKey(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("app.port", 8080, "config.yaml", "yaml", "dev")
	tr.TrackValue("app.port", 9090, "override.yaml", "yaml", "prod")

	output := tr.PrintDebugInfo("app.port")
	assert.NotEmpty(t, output)
	assert.Contains(t, output, "app.port")
	assert.Contains(t, output, "9090")
	assert.Contains(t, output, "Overrides: 1")
	assert.Contains(t, output, "History:")
}

func TestPrintDebugInfo_MissingKey(t *testing.T) {
	tr := NewTracker(false)
	output := tr.PrintDebugInfo("missing")
	assert.Contains(t, output, "not found")
}

func TestPrintDebugInfo_AllKeys(t *testing.T) {
	tr := NewTracker(false)
	tr.TrackValue("a", 1, "s1", "yaml", "")
	tr.TrackValue("b", 2, "s2", "env", "")

	output := tr.PrintDebugInfo("")
	assert.NotEmpty(t, output)
	// Both keys should appear.
	assert.True(t, strings.Contains(output, "a") && strings.Contains(output, "b"),
		"expected both keys in output")
}
