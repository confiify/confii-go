package confii_test

// G10 / Wave 12 coverage for the read-side deep-copy contract.
//
// Wave 11 closed the symmetric write-side contract on [Config.Set]:
// inputs are deep-copied via dictutil.DeepCopyValue so a caller cannot
// mutate the value they passed in and have those mutations alias back
// into envConfig. Wave 12 closes the read-side: every value returned
// from [Config.Get], [Config.ToDict], and [Config.Export] must be a
// deep copy so that
//
//   1. caller mutation cannot leak back into Config state,
//   2. caller mutation cannot bypass [Config.Freeze],
//   3. Export does not race against concurrent Set under -race.
//
// Each test below pins one observable that would fail under the
// pre-Wave-12 behavior (where Get's []any leaf path returned a live
// reference, and Export's pre-Wave-11 form unlocked before marshaling
// the live map).

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	confii "github.com/confiify/confii-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readSideTestLoader is a minimal in-memory loader so tests can pin
// arbitrary shapes (slices at leaves, nested map structures) without
// touching loader/testdata.
type readSideTestLoader struct {
	source string
	data   map[string]any
}

func (l *readSideTestLoader) Load(_ context.Context) (map[string]any, error) {
	return l.data, nil
}
func (l *readSideTestLoader) Source() string { return l.source }

// newReadSideConfig spins up a Config[any] with a single in-memory
// loader carrying the supplied data. Hooks are disabled so the tests
// observe pure pass-through values.
func newReadSideConfig(t *testing.T, data map[string]any) *confii.Config[any] {
	t.Helper()
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(&readSideTestLoader{source: "g10-readside", data: data}),
		confii.WithEnvExpander(false),
		confii.WithTypeCasting(false),
	)
	require.NoError(t, err)
	return cfg
}

// TestG10_ToDict_CallerMutationIsIsolated pins the canonical contract
// surfaced in the audit: mutating the map returned by ToDict must not
// affect a subsequent ToDict call. Pre-Wave-11 ToDict returned the
// live envConfig pointer and `d["foo"] = "x"` would persist.
func TestG10_ToDict_CallerMutationIsIsolated(t *testing.T) {
	cfg := newReadSideConfig(t, map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 5432,
		},
	})

	first := cfg.ToDict()
	first["g10_top_probe"] = "leaked"
	if sub, ok := first["database"].(map[string]any); ok {
		sub["host"] = "evil.example"
	}

	second := cfg.ToDict()
	assert.NotContains(t, second, "g10_top_probe",
		"top-level mutation of ToDict result must not leak into next ToDict call")
	sub, ok := second["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "localhost", sub["host"],
		"nested mutation of ToDict result must not leak into next ToDict call")
}

// TestG10_Get_SubMapMutationIsIsolated pins the same contract for
// whole-map Get on a dot-path. Pre-G11 Get of a sub-tree returned the
// live nested map; mutation aliased into envConfig.
func TestG10_Get_SubMapMutationIsIsolated(t *testing.T) {
	cfg := newReadSideConfig(t, map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 5432,
		},
	})

	v, err := cfg.Get("database")
	require.NoError(t, err)
	first, ok := v.(map[string]any)
	require.True(t, ok)
	first["host"] = "evil.example"
	first["injected"] = "should-not-persist"

	v2, err := cfg.Get("database")
	require.NoError(t, err)
	second, ok := v2.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "localhost", second["host"],
		"caller mutation of Get sub-map must not bleed into envConfig")
	assert.NotContains(t, second, "injected",
		"caller-injected key must not appear on subsequent Get")
	assert.False(t, cfg.Has("database.injected"),
		"caller-injected key must not be visible to Has")
}

// TestG10_Get_SliceLeafMutationIsIsolated pins the previously-uncovered
// []any leaf path. Pre-Wave-12 Get's scalar branch returned the slice
// as-is from c.hookProcessor.ProcessCtx, which is a live reference into
// envConfig. Mutating an element aliased into config state.
func TestG10_Get_SliceLeafMutationIsIsolated(t *testing.T) {
	cfg := newReadSideConfig(t, map[string]any{
		"hosts": []any{"a.local", "b.local", "c.local"},
	})

	v, err := cfg.Get("hosts")
	require.NoError(t, err)
	first, ok := v.([]any)
	require.True(t, ok, "hosts should resolve to []any, got %T", v)
	require.Len(t, first, 3)
	first[0] = "evil.local"

	v2, err := cfg.Get("hosts")
	require.NoError(t, err)
	second, ok := v2.([]any)
	require.True(t, ok)
	assert.Equal(t, "a.local", second[0],
		"caller mutation of Get []any leaf must not bleed into envConfig")
}

// TestG10_Get_NestedSliceInsideMapMutationIsIsolated covers the case
// where a leaf []any sits inside a sub-map returned by Get. The whole-
// map path runs applyHooksRecursive which should produce a deep copy of
// nested slices too.
func TestG10_Get_NestedSliceInsideMapMutationIsIsolated(t *testing.T) {
	cfg := newReadSideConfig(t, map[string]any{
		"cluster": map[string]any{
			"name":    "prod",
			"members": []any{"node-1", "node-2"},
		},
	})

	v, err := cfg.Get("cluster")
	require.NoError(t, err)
	got, ok := v.(map[string]any)
	require.True(t, ok)
	members, ok := got["members"].([]any)
	require.True(t, ok)
	members[0] = "node-evil"

	v2, err := cfg.Get("cluster")
	require.NoError(t, err)
	got2, ok := v2.(map[string]any)
	require.True(t, ok)
	members2, ok := got2["members"].([]any)
	require.True(t, ok)
	assert.Equal(t, "node-1", members2[0],
		"mutation of nested slice returned by whole-map Get must not bleed into envConfig")
}

// TestG10_Freeze_CannotBeBypassedByMutatingReturnedMap proves that
// after Freeze, a caller cannot subvert immutability by reaching into
// the map returned by ToDict / Get and mutating it. Pre-Wave-11 the
// returned map was an alias, so a caller could "set" a key without
// going through Set (which is blocked by Freeze).
func TestG10_Freeze_CannotBeBypassedByMutatingReturnedMap(t *testing.T) {
	cfg := newReadSideConfig(t, map[string]any{
		"database": map[string]any{
			"host": "localhost",
		},
	})

	cfg.Freeze()
	require.True(t, cfg.IsFrozen())

	// Direct Set is blocked.
	require.Error(t, cfg.Set("database.host", "evil"),
		"Freeze must reject Set; fixture precondition")

	// Caller attempts the bypass: grab the map, mutate it.
	d := cfg.ToDict()
	if sub, ok := d["database"].(map[string]any); ok {
		sub["host"] = "evil-via-todict"
	}
	d["g10_freeze_bypass"] = "evil"

	// Verify the live config has not been mutated.
	host, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "localhost", host,
		"caller-side mutation of ToDict result must not bypass Freeze")
	assert.False(t, cfg.Has("g10_freeze_bypass"),
		"Freeze must hold even against ToDict-alias attacks")

	// And via whole-map Get.
	g, err := cfg.Get("database")
	require.NoError(t, err)
	gm, ok := g.(map[string]any)
	require.True(t, ok)
	gm["host"] = "evil-via-get"
	host2, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "localhost", host2,
		"caller-side mutation of Get sub-map result must not bypass Freeze")
}

// TestG10_Export_DoesNotRaceWithConcurrentSet pins the audit's race
// concern: pre-Wave-11 Export released the read lock and then marshaled
// the live envConfig, which raced against a concurrent Set. Post-fix
// Export takes a deep-copy snapshot under the lock and marshals the
// snapshot. Run under -race; failure here would surface as
// "DATA RACE" output, not an assertion miss.
func TestG10_Export_DoesNotRaceWithConcurrentSet(t *testing.T) {
	cfg := newReadSideConfig(t, map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 5432,
		},
		"hosts": []any{"a.local", "b.local"},
	})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: hammer Set on the same key tree Export iterates.
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				_ = cfg.Set("database.host", "host-"+itoa(i))
				_ = cfg.Set("database.port", i)
				i++
			}
		}
	}()

	// Exporter: marshal repeatedly. Any race on envConfig (or on a
	// nested map/slice that escapes the snapshot) would trip -race.
	var exports atomic.Int64
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				b, err := cfg.Export("json")
				if err == nil && len(b) > 0 {
					exports.Add(1)
				}
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()

	assert.Greater(t, exports.Load(), int64(0),
		"the test must actually exercise Export at least once to be meaningful")
}

// TestG10_Export_OutputIsValidUnderConcurrency is a content sanity
// check: the JSON Export emits must be parseable even when a writer is
// hammering Set at the same time. Pre-fix tearing of the live map
// would surface as an unmarshal error.
func TestG10_Export_OutputIsValidUnderConcurrency(t *testing.T) {
	cfg := newReadSideConfig(t, map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 5432,
		},
	})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				_ = cfg.Set("database.host", "host-"+itoa(i))
				i++
			}
		}
	}()

	const iterations = 200
	for i := 0; i < iterations; i++ {
		b, err := cfg.Export("json")
		require.NoError(t, err)
		var sink map[string]any
		require.NoError(t, json.Unmarshal(b, &sink),
			"Export output must be valid JSON under concurrent Set")
	}
	close(stop)
	wg.Wait()
}

// TestG10_ToDict_DoesNotRaceWithConcurrentSet runs ToDict + Set under
// -race. ToDict's deep copy is taken while holding c.mu.RLock so the
// snapshot is atomic with respect to writers; release happens after
// the deep copy is owned privately.
func TestG10_ToDict_DoesNotRaceWithConcurrentSet(t *testing.T) {
	cfg := newReadSideConfig(t, map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 5432,
		},
	})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				_ = cfg.Set("database.host", "host-"+itoa(i))
				i++
			}
		}
	}()

	var dicts atomic.Int64
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				d := cfg.ToDict()
				if d != nil {
					dicts.Add(1)
				}
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()

	assert.Greater(t, dicts.Load(), int64(0))
}

// TestG10_Get_DoesNotRaceWithConcurrentSet runs whole-map and slice-
// leaf Get + Set under -race. The deep copy is taken under the read
// lock, then released; the returned snapshot is owned by the caller.
func TestG10_Get_DoesNotRaceWithConcurrentSet(t *testing.T) {
	cfg := newReadSideConfig(t, map[string]any{
		"cluster": map[string]any{
			"name":    "prod",
			"members": []any{"node-1", "node-2"},
		},
	})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				_ = cfg.Set("cluster.name", "name-"+itoa(i))
				_ = cfg.Set("cluster.members", []any{"node-1", "node-" + itoa(i)})
				i++
			}
		}
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if v, err := cfg.Get("cluster"); err == nil {
					if m, ok := v.(map[string]any); ok {
						// Caller-side mutation: must be isolated and
						// must not race with the writer because the
						// returned map is a deep copy.
						m["caller_only"] = "x"
					}
				}
			}
		}
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if v, err := cfg.Get("cluster.members"); err == nil {
					if s, ok := v.([]any); ok && len(s) > 0 {
						s[0] = "caller-mutation"
					}
				}
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()

	// The "caller_only" key must NEVER have leaked into envConfig
	// despite tens of thousands of mutation attempts.
	assert.False(t, cfg.Has("cluster.caller_only"),
		"caller-side mutation of Get sub-map must never bleed into envConfig")
}

// itoa is a tiny inline-able int->string used in the concurrency tests
// to avoid importing strconv solely for this purpose.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
