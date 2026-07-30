// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type g08Loader struct {
	source string
	data   map[string]any
}

func (l *g08Loader) Load(_ context.Context) (map[string]any, error) {

	return copyMapShallow(l.data), nil
}

func (l *g08Loader) Source() string { return l.source }

func copyMapShallow(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func TestEffectiveSourceIsLoaderNotResolved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "envs.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`default:
  database:
    host: localhost
production:
  database:
    host: prod-db.example.com
`), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
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
		":env-resolved key must NOT report synthetic '(resolved)' source")
	assert.NotEqual(t, "EnvironmentHandler", info.LoaderType,
		":env-resolved key must NOT report 'EnvironmentHandler' as loader_type")

	exp := cfg.Explain("database.host")
	require.True(t, exp["exists"].(bool))
	assert.NotEqual(t, "(resolved)", exp["source"],
		":Explain().source must reflect the actual loader source")
}

func TestReloadClearsStaleKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte("alpha: 1\nbeta: 2\n"), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	require.NotNil(t, cfg.GetSourceInfo("alpha"))
	require.NotNil(t, cfg.GetSourceInfo("beta"))

	require.NoError(t, os.WriteFile(path, []byte("alpha: 1\n"), 0644))

	require.NoError(t, cfg.ReloadWithContext(context.Background(), confii.WithIncremental(false)))

	assert.Nil(t, cfg.GetSourceInfo("beta"),
		":removed key must be evicted from tracker after Reload")
	assert.NotNil(t, cfg.GetSourceInfo("alpha"),
		":surviving key must still be tracked after Reload")

	keys := cfg.FindKeysFromSource(path)
	assert.NotContains(t, keys, "beta",
		":removed key must not appear in FindKeysFromSource after Reload")
	assert.Contains(t, keys, "alpha")
}

func TestReloadDoesNotInflateOverrideCounts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte("k: v\n"), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		require.NoError(t, cfg.ReloadWithContext(context.Background(), confii.WithIncremental(false)))
	}

	info := cfg.GetSourceInfo("k")
	require.NotNil(t, info)
	assert.Equal(t, 0, info.OverrideCount,
		":identical-data reload must not inflate OverrideCount (was %d)",
		info.OverrideCount)
	assert.Empty(t, info.History,
		":identical-data reload must not inflate History (got %d entries)",
		len(info.History))

	exp := cfg.Explain("k")
	_, hasHist := exp["override_history"]
	assert.False(t, hasHist,
		":Explain must not surface override_history for an un-overridden key")
}

func TestLayersResultIsIsolated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte("a: 1\nb: 2\n"), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	first := cfg.Layers()
	require.NotEmpty(t, first)

	first[0]["d08_probe"] = "leaked"
	first[0]["source"] = "tampered"
	if keys, ok := first[0]["keys"].([]string); ok && len(keys) > 0 {
		keys[0] = "tampered_key"
	}

	second := cfg.Layers()
	require.NotEmpty(t, second)

	assert.NotContains(t, second[0], "d08_probe",
		":Layers result must be isolated across calls")
	assert.Equal(t, path, second[0]["source"],
		":source field must not be aliased across Layers calls")

	if keys, ok := second[0]["keys"].([]string); ok {
		for _, k := range keys {
			assert.NotEqual(t, "tampered_key", k,
				":keys slice must not be aliased across Layers calls")
		}
	}
}

func TestExplainResultIsIsolated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`servers:
  - host: a
  - host: b
`), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	first := cfg.Explain("servers")
	require.True(t, first["exists"].(bool))

	first["d08_probe"] = "leaked"
	first["source"] = "tampered"
	first["override_count"] = 999

	if cv, ok := first["current_value"].([]any); ok && len(cv) > 0 {
		if m, ok := cv[0].(map[string]any); ok {
			m["host"] = "tampered.example"
		}

		_ = append(cv, "extra_garbage")
	}

	second := cfg.Explain("servers")
	require.True(t, second["exists"].(bool))

	assert.NotContains(t, second, "d08_probe",
		":Explain result map must be isolated across calls")
	assert.NotEqual(t, "tampered", second["source"],
		":source field must not be aliased across Explain calls")
	assert.NotEqual(t, 999, second["override_count"],
		":override_count must not be aliased across Explain calls")

	cv, ok := second["current_value"].([]any)
	require.True(t, ok)
	require.Len(t, cv, 2)
	first0, _ := cv[0].(map[string]any)
	require.NotNil(t, first0)
	assert.Equal(t, "a", first0["host"],
		":nested current_value entry must not be aliased across Explain calls")

	v, err := cfg.Get("servers")
	require.NoError(t, err)
	servers, ok := v.([]any)
	require.True(t, ok)
	require.Len(t, servers, 2)
	srv0, _ := servers[0].(map[string]any)
	require.NotNil(t, srv0)
	assert.Equal(t, "a", srv0["host"],
		":Explain mutation must not bleed into envConfig (parity)")
}

func TestExplainOverrideHistoryIsIsolated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte("key: v1\n"), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

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
		":override_history element scalars must not be aliased")
	if m, ok := hist2[0]["value"].(map[string]any); ok {
		assert.NotEqual(t, "tampered", m["nested"],
			":override_history element nested values must not be aliased")
	}
}

func TestSchemaResultIsIsolated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`db:
  host: localhost
  port: 5432
`), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	first := cfg.Schema("db")
	require.True(t, first["exists"].(bool))

	if v, ok := first["value"].(map[string]any); ok {
		v["host"] = "tampered.example"
		v["d08_probe"] = "leaked"
	}
	first["d08_top_probe"] = "leaked"

	second := cfg.Schema("db")
	require.True(t, second["exists"].(bool))

	assert.NotContains(t, second, "d08_top_probe",
		":Schema result map must be isolated across calls")
	if v, ok := second["value"].(map[string]any); ok {
		assert.Equal(t, "localhost", v["host"],
			":Schema().value must not be aliased across calls")
		assert.NotContains(t, v, "d08_probe",
			":Schema().value must not be aliased across calls")
	}

	hostVal, err := cfg.Get("db.host")
	require.NoError(t, err)
	assert.Equal(t, "localhost", hostVal,
		":Schema mutation must not bleed into envConfig (parity)")
}

func TestConcurrentReloadAndExplain_Race(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte("k: v0\n"), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	const N = 20
	var wg sync.WaitGroup
	stop := make(chan struct{})

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
			_ = cfg.ReloadWithContext(context.Background(), confii.WithIncremental(false))
		}
	}()

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

	for i := 0; i < N; i++ {

	}

	for i := 0; i < 200; i++ {
		_ = cfg.Explain("k")
	}
	close(stop)
	wg.Wait()

	exp := cfg.Explain("k")
	assert.Equal(t, true, exp["exists"], "/:introspection must report exists=true after concurrent churn")
}
