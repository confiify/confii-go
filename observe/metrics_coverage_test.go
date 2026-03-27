package observe

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetrics_RecordAccess_MultipleDifferentKeys(t *testing.T) {
	m := NewMetrics(5)

	// Record accesses on several keys.
	for i := 0; i < 15; i++ {
		m.RecordAccess("key_a", time.Millisecond)
	}
	for i := 0; i < 10; i++ {
		m.RecordAccess("key_b", 2*time.Millisecond)
	}
	for i := 0; i < 5; i++ {
		m.RecordAccess("key_c", 3*time.Millisecond)
	}

	stats := m.Statistics()
	assert.Equal(t, 3, stats["accessed_keys"])

	top := stats["top_accessed_keys"].(map[string]int)
	assert.Equal(t, 15, top["key_a"])
	assert.Equal(t, 10, top["key_b"])
	assert.Equal(t, 5, top["key_c"])
}

func TestMetrics_RecordAccess_Disabled(t *testing.T) {
	m := NewMetrics(5)
	m.Disable()

	m.RecordAccess("key", time.Millisecond)
	m.RecordAccess("other", time.Millisecond)

	stats := m.Statistics()
	assert.Equal(t, 0, stats["accessed_keys"])
}

func TestMetrics_EnableAfterDisable(t *testing.T) {
	m := NewMetrics(5)
	m.Disable()
	m.RecordAccess("ignored", time.Millisecond)

	m.Enable()
	m.RecordAccess("counted", time.Millisecond)

	stats := m.Statistics()
	assert.Equal(t, 1, stats["accessed_keys"])
	top := stats["top_accessed_keys"].(map[string]int)
	assert.Equal(t, 1, top["counted"])
	_, found := top["ignored"]
	assert.False(t, found)
}

func TestMetrics_RecordReload_AvgDuration(t *testing.T) {
	m := NewMetrics(5)
	m.RecordReload(100 * time.Millisecond)
	m.RecordReload(200 * time.Millisecond)
	m.RecordReload(300 * time.Millisecond)

	stats := m.Statistics()
	assert.Equal(t, 3, stats["reload_count"])
	// Average is (100+200+300)/3 = 200ms.
	assert.Equal(t, (200 * time.Millisecond).String(), stats["avg_reload_time"])
	assert.Contains(t, stats, "last_reload")
}

func TestMetrics_RecordReload_EvictsOldDurations(t *testing.T) {
	m := NewMetrics(5)
	m.maxDurations = 3

	m.RecordReload(100 * time.Millisecond) // will be evicted
	m.RecordReload(200 * time.Millisecond)
	m.RecordReload(300 * time.Millisecond)
	m.RecordReload(400 * time.Millisecond) // triggers eviction of 100ms

	stats := m.Statistics()
	assert.Equal(t, 4, stats["reload_count"])
	// Average of [200, 300, 400] = 300ms.
	assert.Equal(t, (300 * time.Millisecond).String(), stats["avg_reload_time"])
}

func TestMetrics_RecordChange(t *testing.T) {
	m := NewMetrics(5)
	m.RecordChange()
	m.RecordChange()
	m.RecordChange()

	stats := m.Statistics()
	assert.Equal(t, 3, stats["change_count"])
	assert.Contains(t, stats, "last_change")
}

func TestMetrics_Statistics_ZeroKeys(t *testing.T) {
	m := NewMetrics(0)
	m.RecordAccess("key", time.Millisecond)

	stats := m.Statistics()
	// With totalKeys=0, access_rate should be 0 (avoid div by zero).
	assert.Equal(t, float64(0), stats["access_rate"])
}

func TestMetrics_Statistics_AccessRate(t *testing.T) {
	m := NewMetrics(4)
	m.RecordAccess("a", time.Millisecond)
	m.RecordAccess("b", time.Millisecond)

	stats := m.Statistics()
	assert.Equal(t, 0.5, stats["access_rate"])
}

func TestMetrics_Statistics_NoReloadsOrChanges(t *testing.T) {
	m := NewMetrics(5)
	stats := m.Statistics()

	assert.Equal(t, 0, stats["reload_count"])
	assert.Equal(t, 0, stats["change_count"])
	assert.NotContains(t, stats, "last_reload")
	assert.NotContains(t, stats, "last_change")
	assert.Equal(t, (0 * time.Second).String(), stats["avg_reload_time"])
}

func TestMetrics_Statistics_TopKeysLimitedTo10(t *testing.T) {
	m := NewMetrics(20)

	// Create 15 different keys with varying access counts.
	for i := 0; i < 15; i++ {
		key := string(rune('a' + i))
		for j := 0; j <= i; j++ {
			m.RecordAccess(key, time.Millisecond)
		}
	}

	stats := m.Statistics()
	top := stats["top_accessed_keys"].(map[string]int)
	assert.LessOrEqual(t, len(top), 10)
}

func TestMetrics_Reset_ClearsEverything(t *testing.T) {
	m := NewMetrics(10)
	m.RecordAccess("key1", time.Millisecond)
	m.RecordAccess("key2", time.Millisecond)
	m.RecordReload(50 * time.Millisecond)
	m.RecordChange()

	m.Reset()

	stats := m.Statistics()
	assert.Equal(t, 0, stats["accessed_keys"])
	assert.Equal(t, 0, stats["reload_count"])
	assert.Equal(t, 0, stats["change_count"])
	assert.NotContains(t, stats, "last_reload")
	assert.NotContains(t, stats, "last_change")
	top := stats["top_accessed_keys"].(map[string]int)
	assert.Empty(t, top)
}

func TestMetrics_RecordAccess_UpdatesExistingKey(t *testing.T) {
	m := NewMetrics(5)
	m.RecordAccess("mykey", 10*time.Millisecond)
	m.RecordAccess("mykey", 20*time.Millisecond)
	m.RecordAccess("mykey", 30*time.Millisecond)

	m.mu.RLock()
	am := m.accessMetrics["mykey"]
	m.mu.RUnlock()

	require.NotNil(t, am)
	assert.Equal(t, 3, am.AccessCount)
	assert.Equal(t, 60*time.Millisecond, am.TotalAccessTime)
	assert.False(t, am.FirstAccess.IsZero())
	assert.False(t, am.LastAccess.IsZero())
	assert.True(t, am.LastAccess.After(am.FirstAccess) || am.LastAccess.Equal(am.FirstAccess))
}
