// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentStrategyParsing(t *testing.T) {
	for _, value := range []string{"auto", "sectioned", "named_files", "hybrid", " HYBRID "} {
		_, err := parseEnvironmentStrategy(value)
		require.NoError(t, err)
	}
	_, err := parseEnvironmentStrategy("everything")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConfigLoad)
	assert.Contains(t, err.Error(), "invalid environment_strategy")

	for _, value := range []string{"error", "warn", "last_wins", " WARN "} {
		_, err := parseEnvironmentConflictPolicy(value)
		require.NoError(t, err)
	}
	_, err = parseEnvironmentConflictPolicy("silent")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConfigLoad)
	assert.Contains(t, err.Error(), "invalid environment_conflict_policy")
}

func TestResolveEnvironmentStrategyValidation(t *testing.T) {
	environmentFiles := []map[string]any{{"type": "environment_files"}}

	tests := []struct {
		name             string
		strategy         EnvironmentStrategy
		policy           EnvironmentConflictPolicy
		policyConfigured bool
		sources          []map[string]any
		wantStrategy     EnvironmentStrategy
		wantError        string
	}{
		{name: "auto remains selected", strategy: EnvironmentStrategyAuto, policy: EnvironmentConflictLastWins, wantStrategy: EnvironmentStrategyAuto},
		{name: "auto infers named files", strategy: EnvironmentStrategyAuto, policy: EnvironmentConflictLastWins, sources: environmentFiles, wantStrategy: EnvironmentStrategyNamedFiles},
		{name: "sectioned rejects named files", strategy: EnvironmentStrategySectioned, policy: EnvironmentConflictLastWins, sources: environmentFiles, wantError: "cannot be combined"},
		{name: "named files requires source", strategy: EnvironmentStrategyNamedFiles, policy: EnvironmentConflictLastWins, wantError: "requires an environment_files source"},
		{name: "hybrid requires source", strategy: EnvironmentStrategyHybrid, policy: EnvironmentConflictError, policyConfigured: true, wantError: "requires an environment_files source"},
		{name: "hybrid requires policy", strategy: EnvironmentStrategyHybrid, policy: EnvironmentConflictLastWins, sources: environmentFiles, wantError: "requires an explicit"},
		{name: "hybrid valid", strategy: EnvironmentStrategyHybrid, policy: EnvironmentConflictWarn, policyConfigured: true, sources: environmentFiles, wantStrategy: EnvironmentStrategyHybrid},
		{name: "invalid option strategy", strategy: EnvironmentStrategy("invalid"), policy: EnvironmentConflictLastWins, wantError: "invalid environment_strategy"},
		{name: "invalid option policy", strategy: EnvironmentStrategyAuto, policy: EnvironmentConflictPolicy("invalid"), wantError: "invalid environment_conflict_policy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := defaultOptions()
			opts.EnvironmentStrategy = tt.strategy
			opts.EnvironmentConflictPolicy = tt.policy
			opts.environmentConflictPolicyConfigured = tt.policyConfigured
			opts.selfConfigSources = tt.sources
			err := resolveEnvironmentStrategy(&opts)
			if tt.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantStrategy, opts.EnvironmentStrategy)
		})
	}
}

func TestMixedEnvironmentConflictsAndCloning(t *testing.T) {
	writers := map[string][]environmentSourceWriter{
		"server.port": {
			{family: "sectioned", source: "application.yaml"},
			{family: "named_files", source: "default.yaml"},
			{family: "named_files", source: "default.yaml"},
		},
		"server.host": {
			{family: "named_files", source: "production.yaml"},
		},
	}
	conflicts := mixedEnvironmentConflicts(writers)
	require.Len(t, conflicts, 1)
	assert.Equal(t, "server.port", conflicts[0].Key)
	assert.Equal(t, []string{"application.yaml", "default.yaml"}, conflicts[0].Sources)
	assert.Equal(t, "default.yaml", conflicts[0].LastWriter)
	assert.Contains(t, environmentConflictSummary(conflicts), "application.yaml -> default.yaml")

	plan := SourcePlan{
		Layers:    []SourcePlanLayer{{Keys: []string{"server.port"}}},
		Conflicts: conflicts,
	}
	clone := cloneSourcePlan(plan)
	clone.Layers[0].Keys[0] = "changed"
	clone.Conflicts[0].Sources[0] = "changed"
	assert.Equal(t, "server.port", plan.Layers[0].Keys[0])
	assert.Equal(t, "application.yaml", plan.Conflicts[0].Sources[0])
}

func TestEnvironmentStrategyOptions(t *testing.T) {
	opts := defaultOptions()
	WithEnvironmentStrategy(EnvironmentStrategyHybrid)(&opts)
	WithEnvironmentConflictPolicy(EnvironmentConflictWarn)(&opts)
	assert.Equal(t, EnvironmentStrategyHybrid, opts.EnvironmentStrategy)
	assert.Equal(t, EnvironmentConflictWarn, opts.EnvironmentConflictPolicy)
	assert.True(t, opts.isSet("environment_strategy"))
	assert.True(t, opts.isSet("environment_conflict_policy"))
	assert.True(t, opts.environmentConflictPolicyConfigured)
}
