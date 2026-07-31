// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"sync"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveVersion_TOCTOU_RaceFree(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	const goroutines = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	start := make(chan struct{})
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start
			_, err := cfg.SaveVersion(map[string]any{"goroutine": i})
			if err != nil {
				errs <- err
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("SaveVersion returned error: %v", err)
	}

	vm := cfg.EnableVersioning("", 0)
	require.NotNil(t, vm, "EnableVersioning must return the lazily-initialized manager")

	versions := vm.ListVersions()
	assert.Len(t, versions, goroutines,
		"all %d concurrent SaveVersion calls must land in the single shared "+
			"VersionManager — fewer means double-initialization (TOCTOU)",
		goroutines)

	vm2 := cfg.EnableVersioning("", 0)
	assert.Same(t, vm, vm2,
		"EnableVersioning must be idempotent — second call returns the same manager")
}

func TestSaveVersion_FirstCall_InitializesVersioning(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	v, err := cfg.SaveVersion(map[string]any{"first": true})
	require.NoError(t, err, "first-call SaveVersion must lazily initialize versioning")
	require.NotNil(t, v)
	assert.NotEmpty(t, v.VersionID, "VersionID must be populated")
	assert.Equal(t, true, v.Metadata["first"], "metadata must round-trip")

	vm := cfg.EnableVersioning("", 0)
	require.NotNil(t, vm)
	versions := vm.ListVersions()
	require.Len(t, versions, 1, "exactly one version must be recorded")
	assert.Equal(t, v.VersionID, versions[0].VersionID,
		"the recorded version must be the one returned by SaveVersion")
}

func TestSaveVersion_AfterEnableVersioning_Idempotent(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	vmExplicit := cfg.EnableVersioning("", 50)
	require.NotNil(t, vmExplicit)

	v, err := cfg.SaveVersion(map[string]any{"after_explicit": true})
	require.NoError(t, err)
	require.NotNil(t, v)

	vmAfter := cfg.EnableVersioning("", 0)
	assert.Same(t, vmExplicit, vmAfter,
		"SaveVersion must NOT re-initialize the manager when EnableVersioning "+
			"already installed one (idempotence)")

	versions := vmAfter.ListVersions()
	require.Len(t, versions, 1)
	assert.Equal(t, v.VersionID, versions[0].VersionID)
}
