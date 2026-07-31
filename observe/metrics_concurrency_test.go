// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package observe

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMetrics_ConcurrentEnableDisableAndRecord(t *testing.T) {
	m := NewMetrics(50)

	const flips = 1000
	const recorders = 4
	const recordIters = 1000
	const statReaders = 2
	const statIters = 200

	var wg sync.WaitGroup
	wg.Add(1 + recorders + statReaders)

	go func() {
		defer wg.Done()
		for i := 0; i < flips; i++ {
			if i&1 == 0 {
				m.Disable()
			} else {
				m.Enable()
			}
		}

		m.Enable()
	}()

	for r := 0; r < recorders; r++ {
		r := r
		go func() {
			defer wg.Done()
			for i := 0; i < recordIters; i++ {
				m.RecordAccess("key.a", time.Microsecond)
				m.RecordAccess("key.b", time.Microsecond)
				_ = r
			}
		}()
	}

	for r := 0; r < statReaders; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < statIters; i++ {
				_ = m.Statistics()
			}
		}()
	}

	wg.Wait()

	stats := m.Statistics()
	assert.NotNil(t, stats)
	assert.Contains(t, stats, "total_keys")
}

func TestMetrics_RecordsLifecycleOutcomes(t *testing.T) {
	m := NewMetrics(1)

	m.RecordReloadFailed(time.Millisecond)
	m.RecordExtend(time.Millisecond)
	m.RecordExtendFailed(time.Millisecond)
	m.RecordSet()
	m.RecordSetFailed()
	m.RecordOverride()
	m.RecordOverrideFailed()
	m.RecordOverrideRestored()

	stats := m.Statistics()
	assert.Equal(t, 1, stats["reload_failed_count"])
	assert.Equal(t, 1, stats["extend_count"])
	assert.Equal(t, 1, stats["extend_failed_count"])
	assert.Equal(t, 1, stats["set_count"])
	assert.Equal(t, 1, stats["set_failed_count"])
	assert.Equal(t, 1, stats["override_count"])
	assert.Equal(t, 1, stats["override_failed_count"])
	assert.Equal(t, 1, stats["override_restored_count"])
	assert.Contains(t, stats, "last_reload_failed")
	assert.Contains(t, stats, "last_extend")
	assert.Contains(t, stats, "last_extend_failed")
	assert.Contains(t, stats, "last_set")
	assert.Contains(t, stats, "last_set_failed")
	assert.Contains(t, stats, "last_override")
	assert.Contains(t, stats, "last_override_failed")
	assert.Contains(t, stats, "last_override_restored")
}

func TestMetrics_ConcurrentReloadAndChangeAndAccess(t *testing.T) {
	m := NewMetrics(20)

	const iterations = 500
	var wg sync.WaitGroup

	ops := []func(){
		func() { m.RecordReload(time.Millisecond) },
		func() { m.RecordChange() },
		func() { m.RecordAccess("k1", time.Microsecond) },
		func() { m.RecordAccess("k2", time.Microsecond) },
		func() { _ = m.Statistics() },
		func() {

			m.Reset()
		},
	}

	wg.Add(len(ops))
	for i, op := range ops {
		i, op := i, op
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if i == len(ops)-1 && j%50 != 0 {
					continue
				}
				op()
			}
		}()
	}

	wg.Wait()

	stats := m.Statistics()
	assert.NotNil(t, stats)
}

func TestMetrics_DisableHonoredEventually(t *testing.T) {
	m := NewMetrics(10)

	m.RecordAccess("seed", time.Microsecond)

	m.Disable()

	const recorders = 4
	const iterations = 200
	var wg sync.WaitGroup
	var done atomic.Int32
	wg.Add(recorders)

	for r := 0; r < recorders; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				m.RecordAccess("never_recorded", time.Microsecond)
			}
			done.Add(1)
		}()
	}
	wg.Wait()

	stats := m.Statistics()
	accessed := stats["accessed_keys"].(int)
	assert.Equal(t, 1, accessed,
		"Disable must prevent RecordAccess from registering new keys")
}

func TestMetrics_AtomicEnabledFlag_NoMutexContention(t *testing.T) {
	m := NewMetrics(8)

	const goroutines = 16
	const iterations = 2000

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if (g+i)&1 == 0 {
					m.Enable()
				} else {
					m.Disable()
				}
			}
		}()
	}

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				m.RecordAccess("hot.key", time.Nanosecond)
			}
		}()
	}

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations/4; i++ {
				_ = m.Statistics()
			}
		}()
	}

	wg.Wait()

	m.Disable()
	stats := m.Statistics()
	beforeAccessed := stats["accessed_keys"].(int)

	const cooldownIters = 5000
	wg = sync.WaitGroup{}
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < cooldownIters; i++ {
				m.RecordAccess("cold.key", time.Nanosecond)
			}
		}()
	}
	wg.Wait()

	stats = m.Statistics()
	afterAccessed := stats["accessed_keys"].(int)
	assert.Equal(t, beforeAccessed, afterAccessed,
		"once Disable is observed, RecordAccess must take the atomic "+
			"fast path and never register new keys")
}
