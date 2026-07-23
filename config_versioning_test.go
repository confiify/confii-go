package confii_test

// Wave 18 D03 — SaveVersion TOCTOU coverage.
//
// Pre-fix, Config.SaveVersion performed:
//
//	c.mu.RLock()
//	if c.versionMgr == nil {
//	    c.mu.RUnlock()
//	    c.EnableVersioning("", 0) // takes write lock
//	    c.mu.RLock()
//	}
//	... use c.versionMgr ...
//
// Two concurrent first callers could both observe versionMgr == nil
// under RLock, both call EnableVersioning, and (in the worst case)
// race on the write-lock-protected nil check. The fix collapses the
// lazy init into c.versionMgrOnce so initialization is race-clean by
// construction.
//
// The tests below pin three distinct OBSERVABLES that fail under the
// pre-fix code:
//
//  1. TestSaveVersion_TOCTOU_RaceFree: 50 concurrent first callers
//     under -race; all 50 versions present, exactly one VersionManager.
//  2. TestSaveVersion_FirstCall_InitializesVersioning: regression
//     baseline — a single first call initializes versioning correctly.
//  3. TestSaveVersion_AfterEnableVersioning_Idempotent: an explicit
//     EnableVersioning followed by SaveVersion must not re-initialize
//     the manager (preserves the caller-supplied storagePath).

import (
	"context"
	"sync"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSaveVersion_TOCTOU_RaceFree fires N goroutines at SaveVersion on
// a Config with versioning never explicitly enabled. Under -race no
// data race must be reported; functionally, all N versions must be
// recorded by a single VersionManager (verified by ListVersions on the
// manager returned from a post-race EnableVersioning, which is
// idempotent and returns the manager that won the race).
func TestSaveVersion_TOCTOU_RaceFree(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
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
			<-start // align all goroutines on the same starting line
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

	// EnableVersioning is idempotent — it returns the existing manager
	// (or installs a new one if none exists). Because at least one
	// SaveVersion goroutine has already initialized the manager, this
	// call must return that same manager.
	vm := cfg.EnableVersioning("", 0)
	require.NotNil(t, vm, "EnableVersioning must return the lazily-initialized manager")

	// All 50 versions must be present in the single manager. If the
	// pre-fix code had double-initialized (two managers), one set of
	// versions would have been orphaned and we'd see fewer than 50.
	versions := vm.ListVersions()
	assert.Len(t, versions, goroutines,
		"all %d concurrent SaveVersion calls must land in the single shared "+
			"VersionManager — fewer means double-initialization (D03 TOCTOU)",
		goroutines)

	// Sanity: a second EnableVersioning returns the same manager pointer.
	vm2 := cfg.EnableVersioning("", 0)
	assert.Same(t, vm, vm2,
		"EnableVersioning must be idempotent — second call returns the same manager")
}

// TestSaveVersion_FirstCall_InitializesVersioning is the regression
// baseline: a single SaveVersion call on a never-versioned Config must
// successfully initialize versioning and return a usable Version. This
// catches a regression where the sync.Once gate accidentally swallows
// the init.
func TestSaveVersion_FirstCall_InitializesVersioning(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	v, err := cfg.SaveVersion(map[string]any{"first": true})
	require.NoError(t, err, "first-call SaveVersion must lazily initialize versioning")
	require.NotNil(t, v)
	assert.NotEmpty(t, v.VersionID, "VersionID must be populated")
	assert.Equal(t, true, v.Metadata["first"], "metadata must round-trip")

	// EnableVersioning must now return the same manager that SaveVersion
	// just lazily installed (confirms versionMgr is non-nil and shared).
	vm := cfg.EnableVersioning("", 0)
	require.NotNil(t, vm)
	versions := vm.ListVersions()
	require.Len(t, versions, 1, "exactly one version must be recorded")
	assert.Equal(t, v.VersionID, versions[0].VersionID,
		"the recorded version must be the one returned by SaveVersion")
}

// TestSaveVersion_AfterEnableVersioning_Idempotent asserts that calling
// EnableVersioning explicitly THEN SaveVersion does not re-initialize
// the versionMgr — i.e. the manager pointer is preserved across the
// SaveVersion call. The pre-fix code passed this naturally (the nil
// check in EnableVersioning saved it); the sync.Once-based fix must
// preserve the same observable.
func TestSaveVersion_AfterEnableVersioning_Idempotent(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// Explicit enable with a non-default maxVersions; if SaveVersion
	// re-initialized with ("", 0) -> default 100, we'd lose this 50.
	vmExplicit := cfg.EnableVersioning("", 50)
	require.NotNil(t, vmExplicit)

	v, err := cfg.SaveVersion(map[string]any{"after_explicit": true})
	require.NoError(t, err)
	require.NotNil(t, v)

	// Same manager pointer before and after SaveVersion.
	vmAfter := cfg.EnableVersioning("", 0)
	assert.Same(t, vmExplicit, vmAfter,
		"SaveVersion must NOT re-initialize the manager when EnableVersioning "+
			"already installed one (D03 idempotence)")

	// And the saved version must be in that same manager.
	versions := vmAfter.ListVersions()
	require.Len(t, versions, 1)
	assert.Equal(t, v.VersionID, versions[0].VersionID)
}
