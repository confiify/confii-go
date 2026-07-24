// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

// G11: Hook pipeline must apply uniformly across every read-time access
// mode (scalar Get, whole-map Get, Typed, ToDict, Export). Pre-G11 only
// scalar Get walked the hook pipeline; the other access modes returned
// raw envConfig values. These tests pin the post-G11 contract.

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/hook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHooks_CanReenterConfigFromReadPaths verifies that hook execution never
// holds Config's read or write lock. A hook is user code and may legitimately
// update a derived/runtime key; pre-fix, GetCtx/ToDictCtx/ExportCtx held an
// RLock and TypedCtx held the write lock, so the nested Set deadlocked.
func TestHooks_CanReenterConfigFromReadPaths(t *testing.T) {
	tests := map[string]func(context.Context, *confii.Config[secretShape]) error{
		"GetCtx": func(ctx context.Context, cfg *confii.Config[secretShape]) error {
			_, err := cfg.GetCtx(ctx, "foo")
			return err
		},
		"ToDictCtx": func(ctx context.Context, cfg *confii.Config[secretShape]) error {
			_, err := cfg.ToDictCtx(ctx)
			return err
		},
		"ExportCtx": func(ctx context.Context, cfg *confii.Config[secretShape]) error {
			_, err := cfg.ExportCtx(ctx, "json")
			return err
		},
		"TypedCtx": func(ctx context.Context, cfg *confii.Config[secretShape]) error {
			_, err := cfg.TypedCtx(ctx)
			return err
		},
	}

	for name, access := range tests {
		t.Run(name, func(t *testing.T) {
			var cfg *confii.Config[secretShape]
			var once sync.Once
			cfg = newHooksConfig(t, map[string]any{
				"foo": "raw",
			}, func(_ context.Context, _ string, value any) (any, error) {
				var setErr error
				once.Do(func() {
					setErr = cfg.Set("hook.side_effect", "written")
				})
				return value, setErr
			})

			done := make(chan error, 1)
			go func() {
				done <- access(context.Background(), cfg)
			}()

			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("read path deadlocked when a hook re-entered Config.Set")
			}

			got, err := cfg.Get("hook.side_effect")
			require.NoError(t, err)
			assert.Equal(t, "written", got)
		})
	}
}

// hooksTestLoader is a minimal in-memory loader used by the G11 hook
// uniformity tests. It avoids tripping the file-tracker (used by
// reload tests elsewhere) since these tests never touch Reload.
type hooksTestLoader struct {
	source string
	data   map[string]any
}

func (s *hooksTestLoader) Load(_ context.Context) (map[string]any, error) { return s.data, nil }
func (s *hooksTestLoader) Source() string                                 { return s.source }

// secretShape is the typed model used by TestHooks_AppliedUniformly to
// assert that Typed sees hook-resolved values rather than raw
// placeholders.
type secretShape struct {
	Foo     string `mapstructure:"foo"`
	Section struct {
		Foo string `mapstructure:"foo"`
	} `mapstructure:"section"`
}

// newHooksConfig constructs a Config[secretShape] backed by a single
// hooksTestLoader carrying the supplied data and registers the supplied
// FuncCtx as a global context-aware hook.
func newHooksConfig(t *testing.T, data map[string]any, fn hook.FuncCtx) *confii.Config[secretShape] {
	t.Helper()
	cfg, err := confii.New[secretShape](context.Background(),
		confii.WithLoaders(&hooksTestLoader{source: "stub", data: data}),
		// Disable the default hooks so the test sees ONLY the hook we
		// register; otherwise the type-cast hook would coerce
		// "${secret:foo}" via best-effort scalar parsing and confuse
		// the "did MY hook fire?" assertion.
		confii.WithEnvExpander(false),
		confii.WithTypeCasting(false),
	)
	require.NoError(t, err)
	cfg.HookProcessor().RegisterGlobalHookCtx(fn)
	return cfg
}

// TestHooks_AppliedUniformly_AcrossAllAccessModes is the core G11
// regression test. A single context-aware hook resolves the
// placeholder string "${secret:foo}" to the literal "resolved". Every
// access mode (scalar Get, whole-map Get, Typed, ToDict, Export) must
// observe the resolved value.
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
		d := cfg.ToDict()
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

// TestHooks_GetCtx_PropagatesContextToAllAccessPaths verifies that the
// caller's ctx flows into every access path's hook invocation.
// Implementation: a hook records the ctx value it sees on each call;
// the test then asserts every access mode (GetCtx, TypedCtx, ToDictCtx,
// ExportCtx) propagated the caller's ctx unchanged.
func TestHooks_GetCtx_PropagatesContextToAllAccessPaths(t *testing.T) {
	type ctxKey struct{}
	data := map[string]any{
		"foo": "raw",
		"section": map[string]any{
			"foo": "raw",
		},
	}

	var sawValues []any
	recorder := func(ctx context.Context, _ string, v any) (any, error) {
		sawValues = append(sawValues, ctx.Value(ctxKey{}))
		return v, nil
	}
	cfg := newHooksConfig(t, data, recorder)

	mustHaveValue := func(t *testing.T, want string) {
		t.Helper()
		require.NotEmpty(t, sawValues, "expected at least one hook invocation")
		for i, v := range sawValues {
			assert.Equal(t, want, v, "ctx value at hook invocation %d", i)
		}
	}

	t.Run("GetCtx_Scalar", func(t *testing.T) {
		sawValues = nil
		ctx := context.WithValue(context.Background(), ctxKey{}, "scalar")
		_, err := cfg.GetCtx(ctx, "foo")
		require.NoError(t, err)
		mustHaveValue(t, "scalar")
	})

	t.Run("GetCtx_WholeMap", func(t *testing.T) {
		sawValues = nil
		ctx := context.WithValue(context.Background(), ctxKey{}, "wholemap")
		_, err := cfg.GetCtx(ctx, "section")
		require.NoError(t, err)
		mustHaveValue(t, "wholemap")
	})

	t.Run("TypedCtx", func(t *testing.T) {
		sawValues = nil
		ctx := context.WithValue(context.Background(), ctxKey{}, "typed")
		_, err := cfg.TypedCtx(ctx)
		require.NoError(t, err)
		mustHaveValue(t, "typed")
	})

	t.Run("ToDictCtx", func(t *testing.T) {
		sawValues = nil
		ctx := context.WithValue(context.Background(), ctxKey{}, "todict")
		_, err := cfg.ToDictCtx(ctx)
		require.NoError(t, err)
		mustHaveValue(t, "todict")
	})

	t.Run("ExportCtx", func(t *testing.T) {
		sawValues = nil
		ctx := context.WithValue(context.Background(), ctxKey{}, "export")
		_, err := cfg.ExportCtx(ctx, "json")
		require.NoError(t, err)
		mustHaveValue(t, "export")
	})
}

// TestHooks_HookError_FailFast_AcrossAllAccessModes verifies that when
// a context-aware hook returns an error (e.g. a Resolver configured
// with WithResolverFailOnMissing(true) hits an unknown secret), every
// access mode surfaces that error to the caller.
func TestHooks_HookError_FailFast_AcrossAllAccessModes(t *testing.T) {
	sentinel := errors.New("hook-fail-fast")
	data := map[string]any{
		"foo": "${secret:does_not_exist}",
		"section": map[string]any{
			"foo": "${secret:does_not_exist}",
		},
	}

	failer := func(_ context.Context, _ string, v any) (any, error) {
		if s, ok := v.(string); ok && s == "${secret:does_not_exist}" {
			return v, sentinel
		}
		return v, nil
	}
	cfg := newHooksConfig(t, data, failer)

	t.Run("GetCtx_Scalar", func(t *testing.T) {
		_, err := cfg.GetCtx(context.Background(), "foo")
		assert.ErrorIs(t, err, sentinel)
	})

	t.Run("GetCtx_WholeMap", func(t *testing.T) {
		_, err := cfg.GetCtx(context.Background(), "section")
		assert.ErrorIs(t, err, sentinel)
	})

	t.Run("TypedCtx", func(t *testing.T) {
		_, err := cfg.TypedCtx(context.Background())
		require.Error(t, err)
		// TypedCtx wraps the hook error in a *ConfigError via
		// NewValidationError. NewValidationError uses %w on
		// ErrConfigValidation only, so errors.Is(err, sentinel)
		// would fail; assert that the sentinel's message is
		// preserved in the rendered chain so callers can still
		// observe what failed.
		assert.Contains(t, err.Error(), sentinel.Error())
		assert.ErrorIs(t, err, confii.ErrConfigValidation)
	})

	t.Run("ToDictCtx", func(t *testing.T) {
		_, err := cfg.ToDictCtx(context.Background())
		assert.ErrorIs(t, err, sentinel)
	})

	t.Run("ExportCtx", func(t *testing.T) {
		_, err := cfg.ExportCtx(context.Background(), "json")
		assert.ErrorIs(t, err, sentinel)
	})
}

// TestHooks_BackwardCompat_LegacyHookViaProcess verifies that a legacy
// (context-free) hook.Func registered via RegisterGlobalHook still
// applies across every access mode. This guards the contract that the
// G11 fix did not regress legacy hook callers — only added uniformity.
func TestHooks_BackwardCompat_LegacyHookViaProcess(t *testing.T) {
	cfg, err := confii.New[secretShape](context.Background(),
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
	)
	require.NoError(t, err)

	// Legacy non-ctx hook: uppercase strings.
	cfg.HookProcessor().RegisterGlobalHook(func(_ string, v any) any {
		if s, ok := v.(string); ok {
			return s + "_LEGACY"
		}
		return v
	})

	t.Run("ScalarGet", func(t *testing.T) {
		got, err := cfg.Get("foo")
		require.NoError(t, err)
		assert.Equal(t, "raw_LEGACY", got)
	})

	t.Run("WholeMapGet", func(t *testing.T) {
		got, err := cfg.Get("section")
		require.NoError(t, err)
		m := got.(map[string]any)
		assert.Equal(t, "raw_LEGACY", m["foo"])
	})

	t.Run("Typed", func(t *testing.T) {
		model, err := cfg.Typed()
		require.NoError(t, err)
		assert.Equal(t, "raw_LEGACY", model.Foo)
		assert.Equal(t, "raw_LEGACY", model.Section.Foo)
	})

	t.Run("ToDict", func(t *testing.T) {
		d := cfg.ToDict()
		assert.Equal(t, "raw_LEGACY", d["foo"])
		sect := d["section"].(map[string]any)
		assert.Equal(t, "raw_LEGACY", sect["foo"])
	})

	t.Run("Export", func(t *testing.T) {
		out, err := cfg.Export("json")
		require.NoError(t, err)
		var parsed map[string]any
		require.NoError(t, json.Unmarshal(out, &parsed))
		assert.Equal(t, "raw_LEGACY", parsed["foo"])
		sect := parsed["section"].(map[string]any)
		assert.Equal(t, "raw_LEGACY", sect["foo"])
	})
}

// TestHooks_ToDict_DefensiveCopy_PostG11 verifies that the deep-copy
// guarantee implied by the G11 fix holds: caller mutation of the
// ToDict result must not bleed into live state, and a subsequent
// ToDict must not observe the mutation. This is the supporting test
// for the G10/G11 framing in the brief ("applies hooks AND returns a
// defensive copy").
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

	d := cfg.ToDict()
	d["injected"] = "leaked"
	d["nested"].(map[string]any)["b"] = 999

	d2 := cfg.ToDict()
	assert.NotContains(t, d2, "injected", "top-level mutation must not leak")
	assert.Equal(t, 2, d2["nested"].(map[string]any)["b"], "nested mutation must not leak")
}
