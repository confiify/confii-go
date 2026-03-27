package observe

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMetrics_RecordAccess(t *testing.T) {
	m := NewMetrics(10)
	m.RecordAccess("database.host", time.Millisecond)
	m.RecordAccess("database.host", 2*time.Millisecond)
	m.RecordAccess("debug", time.Millisecond)

	stats := m.Statistics()
	assert.Equal(t, 10, stats["total_keys"])
	assert.Equal(t, 2, stats["accessed_keys"])
	top := stats["top_accessed_keys"].(map[string]int)
	assert.Equal(t, 2, top["database.host"])
}

func TestMetrics_RecordReload(t *testing.T) {
	m := NewMetrics(5)
	m.RecordReload(100 * time.Millisecond)
	m.RecordReload(200 * time.Millisecond)

	stats := m.Statistics()
	assert.Equal(t, 2, stats["reload_count"])
	assert.Contains(t, stats, "last_reload")
}

func TestMetrics_Reset(t *testing.T) {
	m := NewMetrics(5)
	m.RecordAccess("key", time.Millisecond)
	m.RecordReload(time.Millisecond)
	m.RecordChange()
	m.Reset()

	stats := m.Statistics()
	assert.Equal(t, 0, stats["accessed_keys"])
	assert.Equal(t, 0, stats["reload_count"])
	assert.Equal(t, 0, stats["change_count"])
}

func TestMetrics_Disabled(t *testing.T) {
	m := NewMetrics(5)
	m.Disable()
	m.RecordAccess("key", time.Millisecond)

	stats := m.Statistics()
	assert.Equal(t, 0, stats["accessed_keys"])
}
