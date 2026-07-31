// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/confiify/confii-go/v2/hook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type nilAdmissionLoader struct{}

func (*nilAdmissionLoader) Load(context.Context) (map[string]any, error) { return nil, nil }
func (*nilAdmissionLoader) Source() string                               { return "nil" }

type nilAdmissionResolver struct{}

func (*nilAdmissionResolver) Hook() hook.Func { return nil }
func (*nilAdmissionResolver) ClearCache()     {}

type panickingAdmissionResolver struct{}

func (*panickingAdmissionResolver) Hook() hook.Func { panic("resolver hook panic") }
func (*panickingAdmissionResolver) ClearCache()     {}

type panickingAdmissionExporter struct{}

func (*panickingAdmissionExporter) Export(map[string]any) ([]byte, error) { return nil, nil }
func (*panickingAdmissionExporter) Format() string                        { panic("format panic") }

func TestNewRejectsInvalidAdmissionWithoutPanicking(t *testing.T) {
	var typedNilLoader *nilAdmissionLoader
	var typedNilResolver *nilAdmissionResolver
	tests := []struct {
		name   string
		option Option
	}{
		{name: "nil option", option: nil},
		{name: "nil loader", option: WithLoaders(nil)},
		{name: "typed nil loader", option: WithLoaders(typedNilLoader)},
		{name: "nil logger", option: WithLogger((*slog.Logger)(nil))},
		{name: "nil global hook", option: WithGlobalHook(nil)},
		{name: "nil condition", option: WithConditionHook(nil, func(context.Context, string, any) (any, error) { return nil, nil })},
		{name: "typed nil resolver", option: WithSecretResolver(typedNilResolver)},
		{name: "invalid error policy", option: WithOnError(ErrorPolicy("unknown"))},
		{name: "invalid merge strategy", option: WithMergeStrategy(MergeStrategy(99))},
		{name: "invalid merge path", option: WithMergeStrategyMap(map[string]MergeStrategy{"database..host": StrategyMerge})},
		{name: "negative startup timeout", option: WithStartupTimeout(-time.Second)},
		{name: "negative operation timeout", option: WithOperationTimeout(-time.Second)},
		{name: "negative reload debounce", option: WithReloadDebounce(-time.Second)},
		{name: "zero secret concurrency", option: WithSecretResolutionConcurrency(0)},
		{name: "conflicting lifecycle", option: func(options *options) {
			options.FreezeOnLoad = true
			options.DynamicReloading = true
		}},
		{name: "panicking option", option: func(*options) { panic("option panic") }},
		{name: "panicking resolver hook", option: WithSecretResolver(&panickingAdmissionResolver{})},
		{name: "panicking exporter format", option: WithExporter(&panickingAdmissionExporter{})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				_, err := New[any](test.option)
				require.Error(t, err)
				require.ErrorIs(t, err, ErrConfigInvalid)
				var configErr *ConfigError
				require.True(t, errors.As(err, &configErr))
			})
		})
	}
}

func TestConstructionOwnsMergeStrategyMap(t *testing.T) {
	strategies := map[string]MergeStrategy{"items": StrategyAppend}
	cfg, err := New[any](
		WithLoaders(
			&contextMapLoader{name: "base", data: map[string]any{"items": []any{"base"}}},
			&contextMapLoader{name: "overlay", data: map[string]any{"items": []any{"overlay"}}},
		),
		WithMergeStrategyMap(strategies),
	)
	require.NoError(t, err)
	strategies["items"] = StrategyReplace

	require.NoError(t, cfg.Reload(WithIncremental(false)))
	value, err := cfg.Get("items")
	require.NoError(t, err)
	assert.Equal(t, []any{"base", "overlay"}, value)
}

func TestValidateAndOwnOptionsRejectsEveryInvalidPlanVariant(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*options)
	}{
		{
			name: "invalid path-specific merge strategy",
			mutate: func(opts *options) {
				opts.MergeStrategyMap = map[string]MergeStrategy{"database": MergeStrategy(99)}
			},
		},
		{
			name: "managed resolver returning nil hook",
			mutate: func(opts *options) {
				opts.SecretResolver = &nilAdmissionResolver{}
				opts.explicitlySet["secret_hook"] = true
			},
		},
		{
			name: "explicit nil secret hook",
			mutate: func(opts *options) {
				opts.SecretHook = nil
				opts.explicitlySet["secret_hook"] = true
			},
		},
		{
			name: "empty key hook path",
			mutate: func(opts *options) {
				opts.hookSetups = []hookSetup{{kind: hookSetupKey, key: "  ", hook: func(context.Context, string, any) (any, error) {
					return nil, nil
				}}}
			},
		},
		{
			name: "empty merge path",
			mutate: func(opts *options) {
				opts.MergeStrategyMap = map[string]MergeStrategy{"": StrategyMerge}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := defaultOptions()
			test.mutate(&opts)
			err := validateAndOwnOptions(&opts)
			require.ErrorIs(t, err, ErrConfigInvalid)
			var configErr *ConfigError
			require.ErrorAs(t, err, &configErr)
			assert.Equal(t, "New", configErr.Op)
		})
	}
}

func TestRuntimeOperationsRejectNilOptionsAndTypedNilLoader(t *testing.T) {
	cfg, err := New[any](WithLoaders(&contextMapLoader{name: "base", data: map[string]any{"key": "value"}}))
	require.NoError(t, err)
	require.ErrorIs(t, cfg.Set("key", "next", nil), ErrConfigInvalid)
	require.ErrorIs(t, cfg.Set("key", "next", func(*setOpts) { panic("set option panic") }), ErrConfigInvalid)
	require.ErrorIs(t, cfg.Reload(nil), ErrConfigInvalid)
	require.ErrorIs(t, cfg.Reload(func(*reloadOpts) { panic("reload option panic") }), ErrConfigInvalid)
	var loader *nilAdmissionLoader
	require.ErrorIs(t, cfg.Extend(loader), ErrConfigInvalid)
}
