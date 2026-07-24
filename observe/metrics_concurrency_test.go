// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package observe

// Concurrency tests for observe.Metrics (Gap G36).
//
// observe/metrics.go has two unguarded accesses to Metrics.enabled:
//   - Enable()  / Disable() (lines ~137-140) write the flag without locking.
//   - RecordAccess() (line ~43) reads the flag outside the mutex.
//
// The pre-existing tests in metrics_test.go are sequential and cannot
// surface that race. The tests below exercise the exact pairing under
// `go test -race`. If `enabled` becomes properly synchronized in the
// future these tests still pass; if a future refactor reintroduces an
// unguarded write to that field, the race detector will catch it here.

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestMetrics_ConcurrentEnableDisableAndRecord asserts that Enable,
// Disable, and RecordAccess can be invoked concurrently without
// producing a -race report on the `enabled` field. Pattern:
//   - 1 goroutine flipping Enable/Disable in a loop (1000 iterations).
//   - 4 goroutines calling RecordAccess (1000 iterations each).
//   - 2 goroutines calling Statistics (which takes the read lock).
//
// Coordination: sync.WaitGroup; no sleeps.
//
// REAL RACE DISCOVERED, OWNED BY GAP G21 ("Observability integration
// is partial and racy"):
//   - observe/metrics.go:137-140: Enable() and Disable() write
//     m.enabled = ... outside the m.mu lock.
//   - observe/metrics.go:43-45: RecordAccess reads m.enabled before
//     acquiring m.mu.
//
// Under `go test -race` this test reproduces the data race in <10ms.
// The G21 fixer owns the production fix in observe/metrics.go (G36 is
// purely test addition and the orchestrator placed a write lock on
// production sources). The G21 fix converted Metrics.enabled to an
// atomic.Bool so this test now runs unconditionally and asserts
// race-freedom under -race.
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
		// Leave metrics enabled at the end so Statistics is meaningful.
		m.Enable()
	}()

	for r := 0; r < recorders; r++ {
		r := r
		go func() {
			defer wg.Done()
			for i := 0; i < recordIters; i++ {
				m.RecordAccess("key.a", time.Microsecond)
				m.RecordAccess("key.b", time.Microsecond)
				_ = r // keep referenced
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

	// The exact counts depend on the Enable/Disable interleaving, but
	// the metric should be in a coherent (non-panicking) state.
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

// TestMetrics_ConcurrentReloadAndChangeAndAccess asserts that
// RecordReload, RecordChange, RecordAccess, Statistics, and Reset can
// all be called concurrently without producing data races on the
// per-metric maps and the durations slice. Pattern: 6 goroutines (one
// per method), 500 iterations each.
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
			// Reset is destructive but must remain safe under
			// concurrent reads/writes. We only reset every 50th
			// iteration to avoid completely starving counters.
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

	// Final Statistics call must not panic and must return a populated
	// map.
	stats := m.Statistics()
	assert.NotNil(t, stats)
}

// TestMetrics_DisableHonoredEventually verifies the post-Disable
// invariant: once Disable returns, subsequent RecordAccess calls must
// not increase accessed_keys. We use atomic counters around a barrier
// channel rather than time.Sleep to avoid flakiness.
func TestMetrics_DisableHonoredEventually(t *testing.T) {
	m := NewMetrics(10)

	// Pre-populate one key so accessed_keys starts at 1.
	m.RecordAccess("seed", time.Microsecond)

	// Disable, then drive a known number of RecordAccess calls.
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

	// Disable was called BEFORE the goroutines launched, so on a
	// correctly synchronized implementation no recorder should have
	// incremented the access map. This holds today because Disable's
	// write is observed by the time the goroutine spawns; after the
	// race fix it will hold deterministically.
	stats := m.Statistics()
	accessed := stats["accessed_keys"].(int)
	assert.Equal(t, 1, accessed,
		"Disable must prevent RecordAccess from registering new keys")
}

// TestMetrics_AtomicEnabledFlag_NoMutexContention exercises the G21
// atomic-bool conversion of Metrics.enabled. It pounds Enable, Disable,
// and RecordAccess from many goroutines and confirms that:
//   - the work completes (no deadlock — disabled-fast-path takes no
//     mutex);
//   - the race detector stays silent (the load/store contract on
//     atomic.Bool is honored at every call site);
//   - the disabled-state fast path actually short-circuits (when we
//     leave the tracker disabled at the end and replay a fresh batch
//     of RecordAccess calls, accessed_keys does not change).
//
// The test is intentionally light on assertions about exact counts —
// the interleaving is non-deterministic — and instead asserts the
// invariants that must hold regardless of scheduler ordering.
func TestMetrics_AtomicEnabledFlag_NoMutexContention(t *testing.T) {
	m := NewMetrics(8)

	const goroutines = 16
	const iterations = 2000

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// Toggler goroutines: flip Enable/Disable rapidly.
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

	// Recorder goroutines: hammer RecordAccess.
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				m.RecordAccess("hot.key", time.Nanosecond)
			}
		}()
	}

	// Reader goroutines: take the read lock concurrently.
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations/4; i++ {
				_ = m.Statistics()
			}
		}()
	}

	wg.Wait()

	// Phase 2: with the tracker definitively disabled, RecordAccess
	// must not mutate the access map. This validates the fast-path
	// short-circuit: a single atomic.Load with no mutex acquisition.
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
