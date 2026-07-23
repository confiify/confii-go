package confii_test

// G12-NewKey-Loaders coverage (Wave 19).
//
// The Wave 11 G12 closure recorded an audit-discovered residual:
// Set claims the synthetic source "runtime" via sourceTracker.TrackValue
// but does NOT register a corresponding fileTracker entry. The Wave 11
// audit text noted this is "consistent with current Reload semantics
// (only file-tracked sources participate in incremental detection) but
// worth surfacing for future work."
//
// Wave 19 investigation (probe outcomes):
//
//  1. When Reload's incremental gate short-circuits (no underlying
//     file mutated), runtime keys persist in envConfig/mergedConfig
//     and OnChange does not fire spuriously.
//  2. When Reload's incremental gate fires (a file changed), Reload
//     rebuilds envConfig from the merged file-loaded data and runtime
//     keys are evicted; OnChange correctly emits (runtimeValue, nil)
//     under the documented union-iteration deletion contract.
//
// Both behaviors are documented Reload semantics: runtime mutations
// are intentionally non-persistent across Reload. The audit was
// theoretical; Wave 19 closes G12-NewKey-Loaders as Option B
// (godoc-pinned divergence, no API changes). The tests below pin
// each observable behavior so regressions surface.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSet_NewKey_DocumentedDivergence pins the Option B contract:
// Set on a brand-new key (a) registers with the source tracker as
// "runtime", and (b) does NOT inject any fileTracker entry that
// would distort the incremental Reload gate. The observable signal
// is that a subsequent no-op Reload (no file mutation) is short-
// circuited by the gate (no callback fires), which would not be the
// case if Set had spuriously perturbed fileTracker.
func TestSet_NewKey_DocumentedDivergence(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	require.NoError(t, cfg.Set("runtime.only.key", "v1"))

	// Source-tracker parity (Wave 11 G12 contract): "runtime" is the
	// recorded source for the new key.
	info := cfg.Explain("runtime.only.key")
	require.Equal(t, true, info["exists"])
	assert.Equal(t, "runtime", info["source"],
		"Set must register runtime source via sourceTracker.TrackValue")

	// fileTracker divergence (G12-NewKey-Loaders Option B): a Reload
	// over UNCHANGED file inputs must hit the incremental short-
	// circuit and fire NO callbacks, even though we just Set a new
	// runtime key. If Set had perturbed fileTracker the gate would
	// either fire or report the runtime path as changed.
	var fired int64
	cfg.OnChange(func(key string, oldVal, newVal any) {
		atomic.AddInt64(&fired, 1)
	})

	require.NoError(t, cfg.Reload(context.Background()))
	assert.Equal(t, int64(0), atomic.LoadInt64(&fired),
		"Reload over unchanged file must short-circuit at incremental gate; "+
			"runtime-Set keys must NOT distort the fileTracker contract")

	// Runtime key still readable post-Reload (no file changed).
	got, gErr := cfg.Get("runtime.only.key")
	require.NoError(t, gErr)
	assert.Equal(t, "v1", got)
}

// TestSet_NewKey_PostReload_RuntimeMutationPersists pins that
// when Reload's incremental gate short-circuits (no underlying file
// changed), a runtime-Set key remains queryable post-Reload. The
// Wave 19 G12-NewKey-Loaders audit raised the concern that runtime
// keys could be silently lost on Reload; this test pins the no-op
// case where the runtime key MUST persist because Reload itself was
// a no-op (gate short-circuit).
func TestSet_NewKey_PostReload_RuntimeMutationPersists(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	require.NoError(t, cfg.Set("runtime.only.A", "rt-A"))
	require.NoError(t, cfg.Set("runtime.only.B", 42))

	// File is unchanged; gate short-circuits; runtime keys persist.
	require.NoError(t, cfg.Reload(context.Background()))

	gotA, errA := cfg.Get("runtime.only.A")
	require.NoError(t, errA)
	assert.Equal(t, "rt-A", gotA,
		"runtime key A must persist across no-op Reload (gate short-circuit)")

	gotB, errB := cfg.Get("runtime.only.B")
	require.NoError(t, errB)
	assert.Equal(t, 42, gotB,
		"runtime key B must persist across no-op Reload")

	// Source tracker still reports "runtime" for both (no Reload
	// retracking happened because the gate short-circuited).
	infoA := cfg.Explain("runtime.only.A")
	assert.Equal(t, "runtime", infoA["source"])
	infoB := cfg.Explain("runtime.only.B")
	assert.Equal(t, "runtime", infoB["source"])
}

// TestSet_NewKey_PostReload_OnChange_FiresCorrectly pins the
// OnChange contract across Set + Reload for a runtime-only key
// under both gate conditions:
//
//   - On Set: OnChange fires once with (nil, runtimeValue) — the
//     Wave 11 G12 / F-G12-Set-CallbackSilence addition contract.
//   - On no-op Reload (gate short-circuits): OnChange does NOT
//     fire for the runtime key.
//   - On file-mutating Reload (gate fires): the runtime-only key is
//     evicted by Phase 4's envConfig rebuild; OnChange fires once
//     with (runtimeValue, nil) — the documented Reload deletion
//     contract.
func TestSet_NewKey_PostReload_OnChange_FiresCorrectly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g12-newkey.yaml")
	require.NoError(t, os.WriteFile(path, []byte("seed: v1\n"), 0600))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	type ev struct {
		key            string
		oldVal, newVal any
	}
	var (
		mu     sync.Mutex
		events []ev
	)
	cfg.OnChange(func(key string, oldVal, newVal any) {
		if key != "runtime.only.A" {
			return
		}
		mu.Lock()
		events = append(events, ev{key, oldVal, newVal})
		mu.Unlock()
	})

	// Set: must fire (nil, "rt-A") exactly once for the runtime key.
	require.NoError(t, cfg.Set("runtime.only.A", "rt-A"))

	mu.Lock()
	require.Len(t, events, 1, "Set on a brand-new key must fire OnChange once")
	assert.Equal(t, "runtime.only.A", events[0].key)
	assert.Nil(t, events[0].oldVal,
		"new-key Set must emit oldVal=nil under union-iteration addition contract")
	assert.Equal(t, "rt-A", events[0].newVal)
	mu.Unlock()

	// No-op Reload: gate short-circuits, OnChange MUST NOT fire for
	// the runtime key. (G12-NewKey-Loaders Option B observable.)
	require.NoError(t, cfg.Reload(context.Background()))

	mu.Lock()
	assert.Len(t, events, 1,
		"no-op Reload (gate short-circuit) must NOT fire OnChange for runtime key")
	mu.Unlock()

	// File-mutating Reload: gate fires, Phase 4 rebuilds envConfig
	// from file data (no runtime key), eviction emits (rt-A, nil).
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, os.WriteFile(path, []byte("seed: v2\n"), 0600))
	require.NoError(t, cfg.Reload(context.Background()))

	mu.Lock()
	require.Len(t, events, 2,
		"file-mutating Reload must fire eviction for runtime-only key")
	assert.Equal(t, "runtime.only.A", events[1].key)
	assert.Equal(t, "rt-A", events[1].oldVal,
		"Reload eviction emits prior runtime value as oldVal")
	assert.Nil(t, events[1].newVal,
		"Reload eviction emits newVal=nil under union-iteration deletion contract")
	mu.Unlock()

	// Post-eviction: runtime key is gone from envConfig.
	_, getErr := cfg.Get("runtime.only.A")
	assert.Error(t, getErr,
		"runtime-only key must be evicted by file-mutating Reload (documented Reload semantics)")
}
