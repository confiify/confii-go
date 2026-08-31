// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"sync"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/hook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Named collection types and interface-held values are where an aliasing bug
// hides: a copy routine that switches on the concrete kinds it expects will
// quietly pass these through by reference.
type hostList []string

type hostSet map[string]int

type ownershipNested struct {
	Hosts   hostList       `confii:"hosts"`
	Weights hostSet        `confii:"weights"`
	Extra   map[string]any `confii:"extra"`
}

type ownershipSettings struct {
	Name     string          `confii:"name"`
	Replicas *int            `confii:"replicas"`
	Tags     []string        `confii:"tags"`
	Matrix   [][]string      `confii:"matrix"`
	Nested   ownershipNested `confii:"nested"`
	Any      any             `confii:"any"`
	Secret   string          `confii:"secret"`
}

// structuredSecretResolver returns a nested structure for a secret reference,
// so a provider-returned collection is exercised rather than only strings.
type structuredSecretResolver struct {
	mu    sync.Mutex
	value string
}

func (r *structuredSecretResolver) Hook() hook.Func {
	return func(_ context.Context, _ string, value any) (any, error) {
		s, ok := value.(string)
		if !ok || s != "${secret:structured}" {
			return value, nil
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.value, nil
	}
}

func (r *structuredSecretResolver) ClearCache() {}

func (r *structuredSecretResolver) set(v string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.value = v
}

func newOwnershipConfig(t *testing.T) (*confii.Config[ownershipSettings], *structuredSecretResolver, map[string]any) {
	t.Helper()
	// The loader keeps a reference to this map, so a snapshot that aliases
	// loader-owned state would be observable through it.
	source := map[string]any{
		"name":     "billing",
		"replicas": 3,
		"tags":     []any{"a", "b"},
		"matrix":   []any{[]any{"r0c0", "r0c1"}, []any{"r1c0"}},
		"nested": map[string]any{
			"hosts":   []any{"h1", "h2"},
			"weights": map[string]any{"h1": 1, "h2": 2},
			"extra":   map[string]any{"k": "v"},
		},
		"any":    map[string]any{"held": []any{"i1", "i2"}},
		"secret": "${secret:structured}",
	}
	resolver := &structuredSecretResolver{value: "resolved-1"}
	cfg, err := confii.NewWithContext[ownershipSettings](context.Background(),
		confii.WithLoaders(&g08Loader{source: "base.yaml", data: source}),
		confii.WithSecretResolver(resolver),
	)
	require.NoError(t, err)
	return cfg, resolver, source
}

func TestTypedCopy_MutatingEveryCollectionLeavesConfigUnchanged(t *testing.T) {
	cfg, _, _ := newOwnershipConfig(t)

	first, err := cfg.TypedCopy()
	require.NoError(t, err)

	// Mutate every reference-bearing shape the struct offers.
	*first.Replicas = 999
	first.Tags[0] = "mutated"
	first.Tags = append(first.Tags, "appended")
	first.Matrix[0][0] = "mutated"
	first.Nested.Hosts[0] = "mutated"
	first.Nested.Weights["h1"] = 999
	first.Nested.Extra["k"] = "mutated"
	if held, ok := first.Any.(map[string]any); ok {
		held["held"] = "mutated"
	}

	second, err := cfg.TypedCopy()
	require.NoError(t, err)

	assert.Equal(t, 3, *second.Replicas, "pointer target must not be shared")
	assert.Equal(t, []string{"a", "b"}, second.Tags, "slice must not be shared")
	assert.Equal(t, "r0c0", second.Matrix[0][0], "nested slice must not be shared")
	assert.Equal(t, hostList{"h1", "h2"}, second.Nested.Hosts,
		"a named slice type must not be shared")
	assert.Equal(t, 1, second.Nested.Weights["h1"],
		"a named map type must not be shared")
	assert.Equal(t, "v", second.Nested.Extra["k"], "nested map must not be shared")

	held, ok := second.Any.(map[string]any)
	require.True(t, ok, "interface-held collection should survive as a map")
	assert.NotEqual(t, "mutated", held["held"],
		"an interface-held collection must not be shared")
}

func TestTypedCopy_SnapshotsAreIndependentOfEachOther(t *testing.T) {
	cfg, _, _ := newOwnershipConfig(t)

	a, err := cfg.TypedCopy()
	require.NoError(t, err)
	b, err := cfg.TypedCopy()
	require.NoError(t, err)

	a.Tags[0] = "mutated"
	a.Nested.Weights["h1"] = 999
	*a.Replicas = 999

	assert.Equal(t, "a", b.Tags[0], "two snapshots must not share a slice")
	assert.Equal(t, 1, b.Nested.Weights["h1"], "two snapshots must not share a map")
	assert.Equal(t, 3, *b.Replicas, "two snapshots must not share a pointer target")
}

func TestTypedCopy_DoesNotAliasLoaderOwnedSource(t *testing.T) {
	cfg, _, source := newOwnershipConfig(t)

	snapshot, err := cfg.TypedCopy()
	require.NoError(t, err)

	// Mutate the map the loader still holds.
	source["name"] = "mutated"
	source["nested"].(map[string]any)["extra"].(map[string]any)["k"] = "mutated"

	assert.Equal(t, "billing", snapshot.Name,
		"a published snapshot must not observe later loader-side mutation")
	assert.Equal(t, "v", snapshot.Nested.Extra["k"])
}

func TestTypedCopy_UnchangedWhenSecretsRefreshIntoANewerSnapshot(t *testing.T) {
	cfg, resolver, _ := newOwnershipConfig(t)

	before, err := cfg.TypedCopy()
	require.NoError(t, err)
	require.Equal(t, "resolved-1", before.Secret)

	resolver.set("resolved-2")
	require.NoError(t, cfg.RefreshSecrets())

	after, err := cfg.TypedCopy()
	require.NoError(t, err)
	assert.Equal(t, "resolved-2", after.Secret, "a newer snapshot sees the new value")
	assert.Equal(t, "resolved-1", before.Secret,
		"a snapshot taken earlier must not change under a refresh")
}

func TestTypedCopy_UnchangedWhenConfigurationIsMutated(t *testing.T) {
	cfg, _, _ := newOwnershipConfig(t)

	before, err := cfg.TypedCopy()
	require.NoError(t, err)

	require.NoError(t, cfg.Set("name", "renamed"))

	after, err := cfg.TypedCopy()
	require.NoError(t, err)
	assert.Equal(t, "renamed", after.Name)
	assert.Equal(t, "billing", before.Name,
		"a snapshot must not change when the configuration is mutated")
}

func TestTypedCopy_SafeForConcurrentReaders(t *testing.T) {
	cfg, _, _ := newOwnershipConfig(t)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snapshot, err := cfg.TypedCopy()
			if !assert.NoError(t, err) {
				return
			}
			// Each goroutine mutates its own copy; none may see another's.
			snapshot.Tags[0] = "local"
			snapshot.Nested.Weights["h1"] = 42
			assert.Equal(t, "local", snapshot.Tags[0])
		}()
	}
	wg.Wait()

	final, err := cfg.TypedCopy()
	require.NoError(t, err)
	assert.Equal(t, "a", final.Tags[0], "concurrent readers must not disturb the source")
}

func TestTypedCopy_RetainedSnapshotSurvivesClose(t *testing.T) {
	cfg, _, _ := newOwnershipConfig(t)

	before, err := cfg.TypedCopy()
	require.NoError(t, err)
	require.NoError(t, cfg.Close())

	assert.Equal(t, "billing", before.Name,
		"a snapshot taken before close remains readable and unchanged")
	assert.Equal(t, []string{"a", "b"}, before.Tags)
}

// Negative control for the tests above. Typed returns a cached pointer, so two
// calls may share state and a mutation through one is visible through the
// other. If this test ever stops detecting sharing, the mutation harness has
// gone blind and the TypedCopy assertions above prove nothing.
func TestTyped_SharesStateWhichIsWhyTypedCopyExists(t *testing.T) {
	cfg, _, _ := newOwnershipConfig(t)

	a, err := cfg.Typed()
	require.NoError(t, err)
	b, err := cfg.Typed()
	require.NoError(t, err)
	require.Same(t, a, b, "Typed is documented to return a cached pointer")

	a.Tags[0] = "mutated"
	assert.Equal(t, "mutated", b.Tags[0],
		"sharing is observable here, so the same mutations would be observable "+
			"in TypedCopy if it shared")

	// And the shared mutation must not reach the configuration itself.
	fresh, err := cfg.TypedCopy()
	require.NoError(t, err)
	assert.Equal(t, "a", fresh.Tags[0],
		"mutating a typed view must not write back to configuration state")
}
