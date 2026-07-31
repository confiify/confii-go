// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/hook"
	"github.com/confiify/confii-go/v2/secret"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type hooksTestLoader struct {
	source string
	data   map[string]any
}

func (s *hooksTestLoader) Load(_ context.Context) (map[string]any, error) { return s.data, nil }
func (s *hooksTestLoader) Source() string                                 { return s.source }

type secretShape struct {
	Foo     string `confii:"foo"`
	Section struct {
		Foo string `confii:"foo"`
	} `confii:"section"`
}

func newHooksConfig(t *testing.T, data map[string]any, fn hook.Func) *confii.Config[secretShape] {
	t.Helper()
	cfg, err := confii.NewWithContext[secretShape](context.Background(),
		confii.WithLoaders(&hooksTestLoader{source: "stub", data: data}),

		confii.WithEnvExpander(false),
		confii.WithTypeCasting(false),
		confii.WithGlobalHook(fn),
	)
	require.NoError(t, err)
	return cfg
}

func TestHooks_AppliedUniformly_AcrossAllAccessModes(t *testing.T) {
	data := map[string]any{
		"foo": "${secret:foo}",
		"section": map[string]any{
			"foo": "${secret:foo}",
		},
	}

	resolver := func(_ context.Context, _ string, v any) (any, error) {
		if s, ok := v.(string); ok && s == "${secret:foo}" {
			return "resolved", nil
		}
		return v, nil
	}
	cfg := newHooksConfig(t, data, resolver)

	t.Run("ScalarGet", func(t *testing.T) {
		got, err := cfg.Get("foo")
		require.NoError(t, err)
		assert.Equal(t, "resolved", got)
	})

	t.Run("WholeMapGet", func(t *testing.T) {
		got, err := cfg.Get("section")
		require.NoError(t, err)
		m, ok := got.(map[string]any)
		require.True(t, ok, "section should resolve to a sub-map, got %T", got)
		assert.Equal(t, "resolved", m["foo"])
	})

	t.Run("Typed", func(t *testing.T) {
		model, err := cfg.Typed()
		require.NoError(t, err)
		assert.Equal(t, "resolved", model.Foo)
		assert.Equal(t, "resolved", model.Section.Foo)
	})

	t.Run("ToDict", func(t *testing.T) {
		d, err := cfg.ToDict()
		require.NoError(t, err)
		assert.Equal(t, "resolved", d["foo"])
		sect, ok := d["section"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "resolved", sect["foo"])
	})

	t.Run("Export_JSON", func(t *testing.T) {
		out, err := cfg.Export("json")
		require.NoError(t, err)
		assert.Contains(t, string(out), `"resolved"`)
		assert.NotContains(t, string(out), "${secret:foo}")
	})

	t.Run("Export_YAML", func(t *testing.T) {
		out, err := cfg.Export("yaml")
		require.NoError(t, err)
		assert.Contains(t, string(out), "resolved")
		assert.NotContains(t, string(out), "${secret:foo}")
	})
}

func TestHooks_CustomTransformRunsBeforeSecretResolution(t *testing.T) {
	resolver := secret.NewResolver(secret.NewDictStore(map[string]any{
		"generated": "resolved-after-custom-hook",
	}))
	cfg, err := confii.New[any](confii.WithLoaders(&hooksTestLoader{source: "stub", data: map[string]any{
		"credential": "secret-alias",
	}}),
		confii.WithEnvExpander(false),
		confii.WithTypeCasting(false),
		confii.WithGlobalHook(func(_ context.Context, _ string, value any) (any, error) {
			if value == "secret-alias" {
				return "${secret:generated}", nil
			}
			return value, nil
		}),
		confii.WithSecretResolver(resolver),
	)
	require.NoError(t, err)

	value, err := cfg.Get("credential")
	require.NoError(t, err)
	assert.Equal(t, "resolved-after-custom-hook", value)
}

func TestHooks_KeyPathAppliesBeforeTypedDecode(t *testing.T) {
	cfg, err := confii.NewWithContext[secretShape](context.Background(),
		confii.WithLoaders(&hooksTestLoader{
			source: "stub",
			data: map[string]any{
				"foo": "top-level",
				"section": map[string]any{
					"foo": "nested",
				},
			},
		}),
		confii.WithEnvExpander(false),
		confii.WithTypeCasting(false),
		confii.WithKeyHook("section.foo", func(_ context.Context, key string, value any) (any, error) {
			assert.Equal(t, "section.foo", key)
			return value.(string) + "-hooked", nil
		}),
	)
	require.NoError(t, err)

	model, err := cfg.Typed()
	require.NoError(t, err)
	assert.Equal(t, "top-level", model.Foo)
	assert.Equal(t, "nested-hooked", model.Section.Foo)
}

func TestHooks_ConstructionContextReachesSnapshotPlan(t *testing.T) {
	type ctxKey struct{}
	var sawValues []any
	recorder := func(ctx context.Context, _ string, v any) (any, error) {
		sawValues = append(sawValues, ctx.Value(ctxKey{}))
		return v, nil
	}
	ctx := context.WithValue(context.Background(), ctxKey{}, "startup")
	_, err := confii.NewWithContext[secretShape](ctx,
		confii.WithLoaders(&hooksTestLoader{source: "stub", data: map[string]any{"foo": "raw"}}),
		confii.WithEnvExpander(false),
		confii.WithTypeCasting(false),
		confii.WithGlobalHook(recorder),
	)
	require.NoError(t, err)
	require.NotEmpty(t, sawValues)
	for index, value := range sawValues {
		assert.Equal(t, "startup", value, "context value at hook invocation %d", index)
	}
}

func TestHooks_HookErrorPreventsInitialSnapshot(t *testing.T) {
	sentinel := errors.New("hook-fail-fast")
	failer := func(_ context.Context, _ string, v any) (any, error) {
		if s, ok := v.(string); ok && s == "${secret:does_not_exist}" {
			return v, sentinel
		}
		return v, nil
	}
	cfg, err := confii.NewWithContext[secretShape](context.Background(),
		confii.WithLoaders(&hooksTestLoader{source: "stub", data: map[string]any{"foo": "${secret:does_not_exist}"}}),
		confii.WithEnvExpander(false),
		confii.WithTypeCasting(false),
		confii.WithGlobalHook(failer),
	)
	assert.Nil(t, cfg)
	assert.ErrorIs(t, err, sentinel)
}

func TestHooks_ReadsDoNotRerunSnapshotPlan(t *testing.T) {
	invocations := 0
	cfg := newHooksConfig(t, map[string]any{"foo": "raw"}, func(_ context.Context, _ string, value any) (any, error) {
		invocations++
		return value, nil
	})
	afterConstruction := invocations
	require.Positive(t, afterConstruction)

	_, err := cfg.Get("foo")
	require.NoError(t, err)
	_, err = cfg.Typed()
	require.NoError(t, err)
	_, err = cfg.ToDict()
	require.NoError(t, err)
	_, err = cfg.Export("json")
	require.NoError(t, err)
	assert.Equal(t, afterConstruction, invocations)
}

func TestHooks_UniformContextHook(t *testing.T) {
	cfg, err := confii.NewWithContext[secretShape](context.Background(),
		confii.WithLoaders(&hooksTestLoader{
			source: "stub",
			data: map[string]any{
				"foo": "raw",
				"section": map[string]any{
					"foo": "raw",
				},
			},
		}),
		confii.WithEnvExpander(false),
		confii.WithTypeCasting(false),
		confii.WithGlobalHook(func(_ context.Context, _ string, v any) (any, error) {
			if s, ok := v.(string); ok {
				return s + "_HOOKED", nil
			}
			return v, nil
		}),
	)
	require.NoError(t, err)

	t.Run("ScalarGet", func(t *testing.T) {
		got, err := cfg.Get("foo")
		require.NoError(t, err)
		assert.Equal(t, "raw_HOOKED", got)
	})

	t.Run("WholeMapGet", func(t *testing.T) {
		got, err := cfg.Get("section")
		require.NoError(t, err)
		m := got.(map[string]any)
		assert.Equal(t, "raw_HOOKED", m["foo"])
	})

	t.Run("Typed", func(t *testing.T) {
		model, err := cfg.Typed()
		require.NoError(t, err)
		assert.Equal(t, "raw_HOOKED", model.Foo)
		assert.Equal(t, "raw_HOOKED", model.Section.Foo)
	})

	t.Run("ToDict", func(t *testing.T) {
		d, err := cfg.ToDict()
		require.NoError(t, err)
		assert.Equal(t, "raw_HOOKED", d["foo"])
		sect := d["section"].(map[string]any)
		assert.Equal(t, "raw_HOOKED", sect["foo"])
	})

	t.Run("Export", func(t *testing.T) {
		out, err := cfg.Export("json")
		require.NoError(t, err)
		var parsed map[string]any
		require.NoError(t, json.Unmarshal(out, &parsed))
		assert.Equal(t, "raw_HOOKED", parsed["foo"])
		sect := parsed["section"].(map[string]any)
		assert.Equal(t, "raw_HOOKED", sect["foo"])
	})
}

func TestHooks_ToDict_DefensiveCopy_PostG11(t *testing.T) {
	cfg := newHooksConfig(t,
		map[string]any{
			"a": 1,
			"nested": map[string]any{
				"b": 2,
			},
		},
		func(_ context.Context, _ string, v any) (any, error) { return v, nil },
	)

	d, err := cfg.ToDict()
	require.NoError(t, err)
	d["injected"] = "leaked"
	d["nested"].(map[string]any)["b"] = 999

	d2, err := cfg.ToDict()
	require.NoError(t, err)
	assert.NotContains(t, d2, "injected", "top-level mutation must not leak")
	assert.Equal(t, 2, d2["nested"].(map[string]any)["b"], "nested mutation must not leak")
}
