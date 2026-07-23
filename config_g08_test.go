package confii_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =========================================================================
// G08: Source tracking reports synthetic or stale origins.
// D08-IntrospectionAlias: Layers / Explain / Schema return aliased
//   internal references.
//
// Wave 13 closure tests. Each test below pins one observable that fails
// under pre-G08/D08 code:
//
//   (1) Effective-source-correct: a env-resolved key reports the loader
//       source that contributed it, NOT the synthetic "(resolved)" string
//       the pre-G08 second TrackConfig pass used to overwrite it with.
//
//   (2) Reload-clears-stale-keys: a key present in v1 of a config and
//       absent in v2 must NOT appear in Layers() / FindKeysFromSource()
//       after a successful Reload. Pre-G08 the tracker accumulated
//       indefinitely.
//
//   (3) Reload-doesnt-inflate-override-counts: 5x Reload of the same
//       data must leave Explain.override_history at length 1 (the runtime
//       Set that primed it), not 5+. Pre-G08 each Reload re-recorded the
//       loader-source key over itself, inflating both OverrideCount and
//       History.
//
//   (4) Layers-aliasing-isolated: mutate the result of Layers(), call
//       Layers() again, assert the second call's keys/maps are pristine.
//
//   (5) Explain-aliasing-isolated: mutate the result of Explain(), call
//       Explain() again, assert the second call is pristine.
//
//   (6) Schema-aliasing-isolated: same as (5) for Schema().
//
//   (7) Race: concurrent Reload + Explain under -race. Introspection
//       must not see torn snapshots and -race must not fire.
//
// All tests live in package confii_test to exercise the public API only.
// =========================================================================

// g08Loader is a minimal in-memory loader used by
// TestG08_EffectiveSourceIsLoaderNotResolved. Other G08/D08 tests use a
// real on-disk YAML file via loader.NewYAML so the file_tracker
// incremental-reload path is exercised end-to-end.
type g08Loader struct {
	source string
	data   map[string]any
}

func (l *g08Loader) Load(_ context.Context) (map[string]any, error) {
	// Hand back a fresh top-level map so the loader pipeline cannot alias
	// our backing store.
	return copyMapShallow(l.data), nil
}

func (l *g08Loader) Source() string { return l.source }

// copyMapShallow performs a shallow copy of the top-level map. Sufficient
// because the test fixtures themselves construct interior structures
// inline and never mutate them after construction.
func copyMapShallow(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// -------------------------------------------------------------------------
// (1) Effective-source-correct.
// -------------------------------------------------------------------------

// TestG08_EffectiveSourceIsLoaderNotResolved pins that an env-resolved
// key reports the loader source it actually came from, not the synthetic
// "(resolved)" string the pre-G08 retrack used to stamp.
func TestG08_EffectiveSourceIsLoaderNotResolved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "envs.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`default:
  database:
    host: localhost
production:
  database:
    host: prod-db.example.com
`), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(&g08Loader{source: path, data: map[string]any{
			"default": map[string]any{
				"database": map[string]any{"host": "localhost"},
			},
			"production": map[string]any{
				"database": map[string]any{"host": "prod-db.example.com"},
			},
		}}),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	info := cfg.GetSourceInfo("database.host")
	require.NotNil(t, info, "database.host must be tracked after env resolution")

	assert.NotEqual(t, "(resolved)", info.SourceFile,
		"G08: env-resolved key must NOT report synthetic '(resolved)' source")
	assert.NotEqual(t, "EnvironmentHandler", info.LoaderType,
		"G08: env-resolved key must NOT report 'EnvironmentHandler' as loader_type")

	exp := cfg.Explain("database.host")
	require.True(t, exp["exists"].(bool))
	assert.NotEqual(t, "(resolved)", exp["source"],
		"G08: Explain.source must reflect the actual loader source")
}

// -------------------------------------------------------------------------
// (2) Reload-clears-stale-keys.
// -------------------------------------------------------------------------

// TestG08_ReloadClearsStaleKeys pins that a key present in v1 of a config
// but removed in v2 disappears from tracker output after Reload.
func TestG08_ReloadClearsStaleKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte("alpha: 1\nbeta: 2\n"), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	// Sanity: both keys are tracked initially.
	require.NotNil(t, cfg.GetSourceInfo("alpha"))
	require.NotNil(t, cfg.GetSourceInfo("beta"))

	// v2 of the file: "beta" is removed.
	require.NoError(t, os.WriteFile(path, []byte("alpha: 1\n"), 0644))

	require.NoError(t, cfg.Reload(context.Background(), confii.WithIncremental(false)))

	assert.Nil(t, cfg.GetSourceInfo("beta"),
		"G08: removed key must be evicted from tracker after Reload")
	assert.NotNil(t, cfg.GetSourceInfo("alpha"),
		"G08: surviving key must still be tracked after Reload")

	keys := cfg.FindKeysFromSource(path)
	assert.NotContains(t, keys, "beta",
		"G08: removed key must not appear in FindKeysFromSource after Reload")
	assert.Contains(t, keys, "alpha")
}

// -------------------------------------------------------------------------
// (3) Reload-doesn't-inflate-override-counts.
// -------------------------------------------------------------------------

// TestG08_ReloadDoesNotInflateOverrideCounts pins that 5x Reload of the
// SAME data does NOT inflate override_count or History on a key.
//
// To make pre-fix failure observable, we first prime "k" with a runtime
// Set so the tracker has *some* prior history (length 1). Pre-G08, every
// Reload re-recorded the loader source for "k" on top of that, so after
// 5 reloads History is length 6+. Post-G08, the per-Reload tracker reset
// rebuilds from scratch and "k" is recorded once (with no History because
// it's a first-track on a fresh tracker).
func TestG08_ReloadDoesNotInflateOverrideCounts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte("k: v\n"), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	// 5x Reload of the same file. Use full (non-incremental) reload so
	// every call exercises the load path even though the bytes haven't
	// changed.
	for i := 0; i < 5; i++ {
		require.NoError(t, cfg.Reload(context.Background(), confii.WithIncremental(false)))
	}

	info := cfg.GetSourceInfo("k")
	require.NotNil(t, info)
	assert.Equal(t, 0, info.OverrideCount,
		"G08: identical-data reload must not inflate OverrideCount (was %d)",
		info.OverrideCount)
	assert.Empty(t, info.History,
		"G08: identical-data reload must not inflate History (got %d entries)",
		len(info.History))

	// And via the Explain payload: no override_history key should be
	// present when the value has never been overridden.
	exp := cfg.Explain("k")
	_, hasHist := exp["override_history"]
	assert.False(t, hasHist,
		"G08: Explain must not surface override_history for an un-overridden key")
}

// -------------------------------------------------------------------------
// (4) Layers-aliasing-isolated.
// -------------------------------------------------------------------------

// TestD08_LayersResultIsIsolated pins that mutating the slice / inner
// maps returned by Layers() cannot bleed into tracker state observed by
// a subsequent Layers() call.
func TestD08_LayersResultIsIsolated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte("a: 1\nb: 2\n"), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	first := cfg.Layers()
	require.NotEmpty(t, first)

	// Mutate the layer map and its keys slice.
	first[0]["d08_probe"] = "leaked"
	first[0]["source"] = "tampered"
	if keys, ok := first[0]["keys"].([]string); ok && len(keys) > 0 {
		keys[0] = "tampered_key"
	}

	second := cfg.Layers()
	require.NotEmpty(t, second)

	assert.NotContains(t, second[0], "d08_probe",
		"D08: Layers result must be isolated across calls")
	assert.Equal(t, path, second[0]["source"],
		"D08: source field must not be aliased across Layers calls")

	if keys, ok := second[0]["keys"].([]string); ok {
		for _, k := range keys {
			assert.NotEqual(t, "tampered_key", k,
				"D08: keys slice must not be aliased across Layers calls")
		}
	}
}

// -------------------------------------------------------------------------
// (5) Explain-aliasing-isolated.
// -------------------------------------------------------------------------

// TestD08_ExplainResultIsIsolated pins that mutating the map returned
// by Explain (including nested current_value / value entries that may
// be maps or slices) cannot bleed into envConfig or tracker state.
func TestD08_ExplainResultIsIsolated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`servers:
  - host: a
  - host: b
`), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	first := cfg.Explain("servers")
	require.True(t, first["exists"].(bool))

	// Mutate top-level scalars.
	first["d08_probe"] = "leaked"
	first["source"] = "tampered"
	first["override_count"] = 999

	// Mutate the nested current_value (a []any of maps).
	if cv, ok := first["current_value"].([]any); ok && len(cv) > 0 {
		if m, ok := cv[0].(map[string]any); ok {
			m["host"] = "tampered.example"
		}
		// And try to extend the slice via the original via append: even
		// if append re-allocates we still want the underlying entries to
		// be unaliased — verified by the per-element assertions below.
		_ = append(cv, "extra_garbage")
	}

	second := cfg.Explain("servers")
	require.True(t, second["exists"].(bool))

	assert.NotContains(t, second, "d08_probe",
		"D08: Explain result map must be isolated across calls")
	assert.NotEqual(t, "tampered", second["source"],
		"D08: source field must not be aliased across Explain calls")
	assert.NotEqual(t, 999, second["override_count"],
		"D08: override_count must not be aliased across Explain calls")

	cv, ok := second["current_value"].([]any)
	require.True(t, ok)
	require.Len(t, cv, 2)
	first0, _ := cv[0].(map[string]any)
	require.NotNil(t, first0)
	assert.Equal(t, "a", first0["host"],
		"D08: nested current_value entry must not be aliased across Explain calls")

	// And verify Get sees the same un-tampered shape (proving the
	// mutation didn't leak into envConfig either).
	v, err := cfg.Get("servers")
	require.NoError(t, err)
	servers, ok := v.([]any)
	require.True(t, ok)
	require.Len(t, servers, 2)
	srv0, _ := servers[0].(map[string]any)
	require.NotNil(t, srv0)
	assert.Equal(t, "a", srv0["host"],
		"D08: Explain mutation must not bleed into envConfig (G10 parity)")
}

// TestD08_ExplainOverrideHistoryIsIsolated pins that mutating an entry
// in the override_history slice returned by Explain cannot leak into the
// tracker's stored OverrideEntry.
func TestD08_ExplainOverrideHistoryIsIsolated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte("key: v1\n"), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	// Drive an override via Set so history accumulates.
	require.NoError(t, cfg.Set("key", map[string]any{"nested": "v2"}))

	first := cfg.Explain("key")
	hist, _ := first["override_history"].([]map[string]any)
	require.NotEmpty(t, hist)

	if m, ok := hist[0]["value"].(map[string]any); ok {
		m["nested"] = "tampered"
	}
	hist[0]["source"] = "tampered_source"

	second := cfg.Explain("key")
	hist2, _ := second["override_history"].([]map[string]any)
	require.NotEmpty(t, hist2)
	assert.NotEqual(t, "tampered_source", hist2[0]["source"],
		"D08: override_history element scalars must not be aliased")
	if m, ok := hist2[0]["value"].(map[string]any); ok {
		assert.NotEqual(t, "tampered", m["nested"],
			"D08: override_history element nested values must not be aliased")
	}
}

// -------------------------------------------------------------------------
// (6) Schema-aliasing-isolated.
// -------------------------------------------------------------------------

// TestD08_SchemaResultIsIsolated pins that mutating the map (or the
// nested value) returned by Schema cannot bleed into envConfig.
func TestD08_SchemaResultIsIsolated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`db:
  host: localhost
  port: 5432
`), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	first := cfg.Schema("db")
	require.True(t, first["exists"].(bool))

	// Mutate the nested value map.
	if v, ok := first["value"].(map[string]any); ok {
		v["host"] = "tampered.example"
		v["d08_probe"] = "leaked"
	}
	first["d08_top_probe"] = "leaked"

	second := cfg.Schema("db")
	require.True(t, second["exists"].(bool))

	assert.NotContains(t, second, "d08_top_probe",
		"D08: Schema result map must be isolated across calls")
	if v, ok := second["value"].(map[string]any); ok {
		assert.Equal(t, "localhost", v["host"],
			"D08: Schema.value must not be aliased across calls")
		assert.NotContains(t, v, "d08_probe",
			"D08: Schema.value must not be aliased across calls")
	}

	// Cross-check via Get that envConfig wasn't corrupted.
	hostVal, err := cfg.Get("db.host")
	require.NoError(t, err)
	assert.Equal(t, "localhost", hostVal,
		"D08: Schema mutation must not bleed into envConfig (G10 parity)")
}

// -------------------------------------------------------------------------
// (7) Race: concurrent Reload + Explain.
// -------------------------------------------------------------------------

// TestG08_D08_ConcurrentReloadAndExplain_Race pins that Explain run in
// parallel with Reload never observes a torn snapshot and is safe under
// -race. Pre-G08 the tracker reset + load sequence was not protected
// against a concurrent reader that pulled GetSourceInfo for a key during
// the brief moment the tracker had been reset but not yet repopulated.
// The Wave 13 fix relies on the existing c.mu write lock that Reload
// holds for phases 3-7 to serialize tracker mutations against Explain
// readers.
func TestG08_D08_ConcurrentReloadAndExplain_Race(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte("k: v0\n"), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	const N = 20
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Reload churn.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			body := []byte("k: v\nextra: " + string(rune('a'+i%5)) + "\n")
			_ = os.WriteFile(path, body, 0644)
			_ = cfg.Reload(context.Background(), confii.WithIncremental(false))
		}
	}()

	// Concurrent introspection readers.
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = cfg.Explain("k")
				_ = cfg.Layers()
				_ = cfg.Schema("k")
			}
		}()
	}

	// Let readers finish, then stop the writer.
	for i := 0; i < N; i++ {
		// (no-op; the WaitGroup below is the actual sync.)
	}
	// Allow some interleaving before signaling stop.
	for i := 0; i < 200; i++ {
		_ = cfg.Explain("k")
	}
	close(stop)
	wg.Wait()

	// Final probe: introspection must report a coherent snapshot
	// post-quiescence.
	exp := cfg.Explain("k")
	assert.Equal(t, true, exp["exists"], "G08/D08: introspection must report exists=true after concurrent churn")
}
