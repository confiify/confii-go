// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	confii "github.com/confiify/confii-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type readSideTestLoader struct {
	source string
	data   map[string]any
}

func (l *readSideTestLoader) Load(_ context.Context) (map[string]any, error) {
	return l.data, nil
}
func (l *readSideTestLoader) Source() string { return l.source }

func newReadSideConfig(t *testing.T, data map[string]any) *confii.Config[any] {
	t.Helper()
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(&readSideTestLoader{source: "g10-readside", data: data}),
		confii.WithEnvExpander(false),
		confii.WithTypeCasting(false),
	)
	require.NoError(t, err)
	return cfg
}

func TestToDict_CallerMutationIsIsolated(t *testing.T) {
	cfg := newReadSideConfig(t, map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 5432,
		},
	})

	first, err := cfg.ToDict()
	require.NoError(t, err)
	first["g10_top_probe"] = "leaked"
	if sub, ok := first["database"].(map[string]any); ok {
		sub["host"] = "evil.example"
	}

	second, err := cfg.ToDict()
	require.NoError(t, err)
	assert.NotContains(t, second, "g10_top_probe",
		"top-level mutation of ToDict result must not leak into next ToDict call")
	sub, ok := second["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "localhost", sub["host"],
		"nested mutation of ToDict result must not leak into next ToDict call")
}

func TestGet_SubMapMutationIsIsolated(t *testing.T) {
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

func TestGet_SliceLeafMutationIsIsolated(t *testing.T) {
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

func TestGet_NestedSliceInsideMapMutationIsIsolated(t *testing.T) {
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

func TestFreeze_CannotBeBypassedByMutatingReturnedMap(t *testing.T) {
	cfg := newReadSideConfig(t, map[string]any{
		"database": map[string]any{
			"host": "localhost",
		},
	})

	cfg.Freeze()
	require.True(t, cfg.IsFrozen())

	require.Error(t, cfg.Set("database.host", "evil"),
		"Freeze must reject Set(); fixture precondition")

	d, err := cfg.ToDict()
	require.NoError(t, err)
	if sub, ok := d["database"].(map[string]any); ok {
		sub["host"] = "evil-via-todict"
	}
	d["g10_freeze_bypass"] = "evil"

	host, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "localhost", host,
		"caller-side mutation of ToDict result must not bypass Freeze")
	assert.False(t, cfg.Has("g10_freeze_bypass"),
		"Freeze must hold even against ToDict-alias attacks")

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

func TestExport_DoesNotRaceWithConcurrentSet(t *testing.T) {
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

func TestExport_OutputIsValidUnderConcurrency(t *testing.T) {
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

func TestToDict_DoesNotRaceWithConcurrentSet(t *testing.T) {
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
				d, err := cfg.ToDict()
				if err != nil {
					return
				}
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

func TestGet_DoesNotRaceWithConcurrentSet(t *testing.T) {
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

	assert.False(t, cfg.Has("cluster.caller_only"),
		"caller-side mutation of Get sub-map must never bleed into envConfig")
}

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
