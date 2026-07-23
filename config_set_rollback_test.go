package confii_test

// F-Set-RollbackFidelity (Wave 17) coverage for Config.Set's failure-path
// rollback. Pre-Wave 17 the rollback path was:
//
//	c.envConfig = dictutil.Unflatten(dictutil.Flatten(c.envConfig))
//
// which is lossy along multiple axes:
//
//  1. Type-degradation. Flatten/Unflatten round-trips the value graph
//     through the flat-key representation. For non-string scalar types
//     (int, time.Duration, custom typed integers) the round-trip is
//     identity at the value level only because Flatten preserves the
//     interface{} pointer — but if a downstream restore ever marshalled
//     to YAML/JSON and back the type would be collapsed. More importantly,
//     a nil leaf and a missing key are indistinguishable in the flat
//     representation: pre-Wave 17 a key explicitly bound to nil could
//     vanish from the rollback result.
//
//  2. Sub-tree-Set lossy. If Set is invoked with a map value at a key,
//     the rollback must restore the ORIGINAL sub-tree shape (key
//     ordering, nested map identity, sibling keys not touched by Set).
//     Flatten collapses ordering and explicit-empty-map markers.
//
//  3. mergedConfig divergence. Pre-Wave 17 the rollback path only
//     restored envConfig — mergedConfig was left in whatever partial
//     state SetNested produced before failing. Post-Wave 17 both maps
//     are restored from a deep-copy snapshot taken before either
//     SetNested call.
//
//  4. First-call rollback. dictutil.SetNested may insert intermediate
//     empty maps before encountering a non-map traversal and returning
//     a *PathError. Pre-Wave 17 the FIRST SetNested call's failure path
//     skipped rollback entirely, so a rejected Set could leave empty
//     intermediate maps in envConfig. Post-Wave 17 both failure paths
//     restore envConfig from the deep-copy snapshot.
//
// Each test below pins one observable that would fail under the
// pre-Wave 17 Unflatten(Flatten(...)) rollback.

import (
	"context"
	"testing"
	"time"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSet_RollbackFidelity_TypePreservation_Int pins type-fidelity on
// the Set rollback path. After a successful Set("port", 8080) and a
// FAILED Set("port.subkey", "x") (which traverses through the int
// scalar), Get("port") must still return 8080 as an int — not as a
// float64 (the YAML/JSON round-trip type) and not as a missing key.
//
// Pre-Wave 17 the rollback was structurally OK for the in-process
// representation (Flatten/Unflatten preserves interface{} identity),
// but the rollback path was only triggered on the SECOND SetNested
// failure (mergedConfig) and only restored envConfig. The first failure
// path (envConfig) had NO rollback at all, so SetNested's intermediate
// map insertions could leak. This test forces the rollback to fire and
// asserts the int type survives.
func TestSet_RollbackFidelity_TypePreservation_Int(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// Establish an int-typed leaf via Set so we control the input type.
	require.NoError(t, cfg.Set("port", 8080))

	pre, err := cfg.Get("port")
	require.NoError(t, err)
	require.IsType(t, int(0), pre,
		"precondition: Set must store int as int (no Flatten/Unflatten on the success path)")

	// Force a path-error rollback: traverse THROUGH the int scalar.
	// dictutil.SetNested returns *PathError on the FIRST SetNested call
	// (against envConfig), exercising the new first-failure rollback.
	err = cfg.Set("port.subkey", "x")
	require.Error(t, err, "Set through a scalar must error (G12)")

	post, err := cfg.Get("port")
	require.NoError(t, err,
		"after rollback the original int leaf must still be queryable")
	assert.IsType(t, int(0), post,
		"F-Set-RollbackFidelity: rollback must preserve int type (not collapse to float64)")
	assert.Equal(t, 8080, post,
		"F-Set-RollbackFidelity: rollback must preserve int value")
}

// TestSet_RollbackFidelity_TypePreservation_Duration pins type-fidelity
// for time.Duration — the canonical "lossy under YAML/JSON" type that
// the audit text calls out explicitly.
func TestSet_RollbackFidelity_TypePreservation_Duration(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	want := 5 * time.Second
	require.NoError(t, cfg.Set("timeout", want))

	pre, err := cfg.Get("timeout")
	require.NoError(t, err)
	require.IsType(t, time.Duration(0), pre,
		"precondition: time.Duration must round-trip as Duration")

	// Force the rollback by traversing through the Duration scalar.
	err = cfg.Set("timeout.subkey", "x")
	require.Error(t, err)

	post, err := cfg.Get("timeout")
	require.NoError(t, err,
		"after rollback the original Duration leaf must still be queryable")
	assert.IsType(t, time.Duration(0), post,
		"F-Set-RollbackFidelity: rollback must preserve time.Duration type (not collapse to int64)")
	assert.Equal(t, want, post)
}

// TestSet_RollbackFidelity_NilLeafDistinction pins the nil-leaf
// distinction — a key explicitly bound to nil vs a missing key. After
// a Set("explicit_nil", nil) and a failing Set that traverses through
// a different scalar, Has("explicit_nil") must remain true. Pre-Wave 17
// the Flatten step might drop nil leaves, making the rollback indistinguishable
// from "key never existed".
func TestSet_RollbackFidelity_NilLeafDistinction(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// Bind the key to an explicit nil.
	require.NoError(t, cfg.Set("explicit_nil", nil))
	require.True(t, cfg.Has("explicit_nil"),
		"precondition: Set(key, nil) must register the key as present")

	// Force a rollback via a separate scalar traversal.
	require.NoError(t, cfg.Set("port", 8080))
	err = cfg.Set("port.subkey", "x")
	require.Error(t, err)

	assert.True(t, cfg.Has("explicit_nil"),
		"F-Set-RollbackFidelity: rollback must preserve nil-leaf distinction (key with explicit nil != missing key)")
}

// TestSet_RollbackFidelity_SubTreePreservation pins sub-tree shape on
// rollback. Set is documented to deep-copy map values
// (TestSet_DeepCopiesMapValue, F-G10-SetInputAlias), so a sub-tree Set
// is currently supported — even though the audit text speculates about
// it as a "future extension". This test exercises that path: write a
// sub-tree at "service" via Set, then force a failing Set that
// traverses through a scalar elsewhere; the original "service"
// sub-tree must survive identically.
func TestSet_RollbackFidelity_SubTreePreservation(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// Sub-tree Set: "service" gets a nested map. Includes an explicit
	// empty sub-map ("plugins") — pre-Wave 17 the Flatten step would
	// drop empty sub-maps from the rollback because Flatten only emits
	// LEAF entries (an empty map has no leaves and so is invisible to
	// Flatten/Unflatten round-trips). Post-Wave 17 the structural
	// deep-copy snapshot preserves the empty sub-map intact.
	subTree := map[string]any{
		"host": "primary.example.com",
		"port": 9090,
		"tls": map[string]any{
			"enabled":  true,
			"min_ver":  "1.3",
			"min_port": 4433,
		},
		"plugins": map[string]any{}, // explicit empty sub-tree
	}
	require.NoError(t, cfg.Set("service", subTree))
	require.True(t, cfg.Has("service.plugins"),
		"precondition: explicit empty sub-map must be reachable via Has")

	// Force a rollback elsewhere.
	require.NoError(t, cfg.Set("port", 8080))
	err = cfg.Set("port.subkey", "x")
	require.Error(t, err)

	// The "service" sub-tree must be intact.
	host, err := cfg.GetString("service.host")
	require.NoError(t, err)
	assert.Equal(t, "primary.example.com", host)

	port, err := cfg.Get("service.port")
	require.NoError(t, err)
	assert.IsType(t, int(0), port,
		"sub-tree leaf int must remain int after rollback")
	assert.Equal(t, 9090, port)

	tlsEnabled, err := cfg.GetBool("service.tls.enabled")
	require.NoError(t, err)
	assert.True(t, tlsEnabled)

	tlsMinPort, err := cfg.Get("service.tls.min_port")
	require.NoError(t, err)
	assert.IsType(t, int(0), tlsMinPort,
		"deeply nested sub-tree int must remain int after rollback")
	assert.Equal(t, 4433, tlsMinPort)

	// The explicit empty sub-map ("service.plugins") must survive the
	// rollback. Pre-Wave 17 the Flatten/Unflatten round-trip would have
	// silently dropped it because empty sub-maps emit no leaves — this
	// is the canonical observable that DOES diverge between the lossy
	// Flatten/Unflatten rollback and the structural Snapshot/Restore.
	// Even though the rollback didn't fire on this sub-tree (the failure
	// was on an unrelated key), the contract guarantees that ANY rollback
	// is structural for the full envConfig/mergedConfig.
	assert.True(t, cfg.Has("service.plugins"),
		"F-Set-RollbackFidelity: explicit empty sub-map must survive rollback (pre-Wave 17 Flatten/Unflatten dropped empty sub-trees)")
}

// TestSet_RollbackFidelity_EmptySubMapSurvivesActiveRollback is the
// active-rollback variant of the empty-sub-map test: it forces the
// rollback to actually fire (via a path-error Set) AFTER an empty
// sub-map has been written, then asserts the empty sub-map is still
// reachable. Pre-Wave 17 the Flatten/Unflatten reconstruction would
// have silently dropped the empty sub-map; post-Wave 17 the structural
// deep-copy snapshot preserves it.
func TestSet_RollbackFidelity_EmptySubMapSurvivesActiveRollback(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// Write an explicit empty sub-map.
	require.NoError(t, cfg.Set("plugins", map[string]any{}))
	require.True(t, cfg.Has("plugins"),
		"precondition: empty sub-map must be Has-reachable")

	// Force a rollback via a path-error Set elsewhere.
	err = cfg.Set("debug.feature", true) // debug is bool
	require.Error(t, err)

	// Empty sub-map must still be reachable.
	assert.True(t, cfg.Has("plugins"),
		"F-Set-RollbackFidelity: rollback after empty sub-map Set must preserve the empty sub-map (pre-Wave 17 Flatten/Unflatten would have dropped it)")
}

// TestSet_RollbackFidelity_FirstSetNestedFailure_NoIntermediates pins
// the first-call rollback contract. Pre-Wave 17 a Set that failed on
// the FIRST SetNested call (envConfig) had NO rollback at all, so any
// intermediate empty maps SetNested inserted before encountering the
// non-map traversal would leak into envConfig. Post-Wave 17 envConfig
// is restored from the deep-copy snapshot, so a failing Set leaves no
// observable trace.
func TestSet_RollbackFidelity_FirstSetNestedFailure_NoIntermediates(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// Snapshot pre-Set keys for diff.
	preKeys := map[string]struct{}{}
	for _, k := range cfg.Keys() {
		preKeys[k] = struct{}{}
	}

	// Set that should fail on path traversal: "debug" is bool.
	// SetNested may insert an "a.b.c" prefix as empty maps before
	// reaching the non-map node — we want to assert that NO new keys
	// or intermediate map shells appear in envConfig after the failure.
	err = cfg.Set("debug.feature.flag.deep.path", true)
	require.Error(t, err)

	postKeys := map[string]struct{}{}
	for _, k := range cfg.Keys() {
		postKeys[k] = struct{}{}
	}

	// Symmetric diff must be empty: rollback removed every intermediate.
	for k := range postKeys {
		_, ok := preKeys[k]
		assert.True(t, ok,
			"F-Set-RollbackFidelity: failed Set must not leak key %q (rollback regressed)", k)
	}
	for k := range preKeys {
		_, ok := postKeys[k]
		assert.True(t, ok,
			"F-Set-RollbackFidelity: failed Set must not delete pre-existing key %q", k)
	}
}

// TestSet_RollbackFidelity_TrackerNoStaleRuntimeEntry pins tracker
// rollback symmetry with Wave 16 Override. The current control flow
// places TrackValue AFTER both SetNested calls succeed, so a SetNested
// failure on either path means TrackValue never fired and Explain(key)
// MUST NOT report a "runtime" source for the rejected key. This test
// is the regression guard against any future reordering that would
// move TrackValue ahead of a SetNested call.
func TestSet_RollbackFidelity_TrackerNoStaleRuntimeEntry(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// "debug" is bool — traversing through it is a guaranteed PathError.
	err = cfg.Set("debug.feature.flag", true)
	require.Error(t, err)

	// The rejected leaf key must not exist...
	assert.False(t, cfg.Has("debug.feature.flag"),
		"failed Set must not leave the rejected key reachable")

	// ...and Explain on the rejected key must not claim "runtime".
	info := cfg.Explain("debug.feature.flag")
	if info["exists"] == true {
		assert.NotEqual(t, "runtime", info["source"],
			"F-Set-RollbackFidelity: failed Set must not leave a stale 'runtime' tracker entry")
	}
}

// TestSet_RollbackFidelity_EnvAndMergedRemainInLockstep pins the
// env-vs-merged divergence guard. Pre-Wave 17 the rollback path
// restored envConfig but left mergedConfig in whatever partial state
// SetNested produced before failing. Post-Wave 17 both maps are
// restored from independent deep-copy snapshots taken pre-mutation.
//
// Observable: a failing Set must leave Get(key) and any merge-derived
// downstream view in agreement on the key set.
func TestSet_RollbackFidelity_EnvAndMergedRemainInLockstep(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	preDict := cfg.ToDict()

	// Force rollback: "debug" is bool, "debug.x" traverses through it.
	err = cfg.Set("debug.x.y", true)
	require.Error(t, err)

	postDict := cfg.ToDict()

	// Effective configuration (which is built from mergedConfig +
	// envConfig + hooks) must be unchanged.
	assert.Equal(t, preDict, postDict,
		"F-Set-RollbackFidelity: failed Set must leave envConfig and mergedConfig in lockstep with their pre-Set snapshots")
}
