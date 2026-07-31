// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"testing"

	"github.com/confiify/confii-go/v2/observe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigOwnedCollaboratorsExposeOnlyRequiredCapabilities(t *testing.T) {
	cfg, err := New[any]()
	require.NoError(t, err)

	metrics := cfg.EnableObservability()
	require.NotNil(t, metrics)
	_, exposesMutableMetrics := any(metrics).(*observe.Metrics)
	assert.False(t, exposesMutableMetrics)
	assert.Same(t, metrics, cfg.EnableObservability())

	events := cfg.EnableEvents()
	require.NotNil(t, events)
	_, exposesEmitter := any(events).(*observe.EventEmitter)
	assert.False(t, exposesEmitter)
	assert.Same(t, events, cfg.EnableEvents())

	versions := cfg.EnableVersioning("", 5)
	require.NotNil(t, versions)
	_, exposesManager := any(versions).(*observe.VersionManager)
	assert.False(t, exposesManager)
	assert.Same(t, versions, cfg.EnableVersioning("", 5))
}

func TestConfigCapabilityViewsRemainOperational(t *testing.T) {
	cfg, err := New[any]()
	require.NoError(t, err)

	metrics := cfg.EnableObservability()
	require.NoError(t, cfg.Set("server.port", 8080))
	assert.Equal(t, 1, metrics.Statistics()["set_count"])
	cfg.DisableObservability()
	require.NoError(t, cfg.Set("server.port", 9090))
	assert.Equal(t, 1, metrics.Statistics()["set_count"], "disabled metrics must not collect lifecycle operations")
	cfg.ResetMetrics()
	assert.Equal(t, 0, metrics.Statistics()["set_count"])

	events := cfg.EnableEvents()
	called := 0
	events.OnWithContext("set", func(ctx context.Context, _ ...any) {
		require.NotNil(t, ctx)
		called++
	})
	require.NoError(t, cfg.Set("server.port", 7070))
	assert.Equal(t, 1, called)
	events.OffWithContext("set")
	require.NoError(t, cfg.Set("server.port", 6060))
	assert.Equal(t, 1, called)

	// The non-context On/Off pair is part of the frozen EventSubscriber surface
	// and must register and remove listeners symmetrically.
	plain := 0
	events.On("set", func(...any) { plain++ })
	require.NoError(t, cfg.Set("server.port", 6161))
	assert.Equal(t, 1, plain)
	events.Off("set")
	require.NoError(t, cfg.Set("server.port", 6262))
	assert.Equal(t, 1, plain, "Off must stop further delivery to a non-context listener")

	versions := cfg.EnableVersioning("", 5)
	first, err := cfg.SaveVersion(nil)
	require.NoError(t, err)
	require.NoError(t, cfg.Set("server.port", 5050))
	second, err := cfg.SaveVersion(nil)
	require.NoError(t, err)
	assert.Len(t, versions.ListVersions(), 2)
	require.NotNil(t, versions.LatestVersion())
	assert.Equal(t, second.VersionID, versions.LatestVersion().VersionID,
		"LatestVersion must report the most recently saved snapshot")
	differences, err := versions.DiffVersions(first.VersionID, second.VersionID)
	require.NoError(t, err)
	assert.NotEmpty(t, differences)
}
