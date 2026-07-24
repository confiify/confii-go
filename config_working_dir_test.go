// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
	"github.com/confiify/confii-go/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Wave 22 Validated Gap G06: Self-config discovery is coupled to global CWD
// and a fragile cache.
//
// Pre-G06 issues:
//  - applySelfConfig always called selfconfig.Read(".") (config.go:1025)
//  - The composer was always created with compose.New(".") (config.go:105)
//  - selfconfig.Read only cached the literal "." key, so two concurrent
//    callers with different working directories shared a single cache slot
//
// Fix (G06):
//  - Added WithWorkingDir(string) Option whose value is threaded into both
//    selfconfig.Read and compose.New.
//  - selfconfig now keys its cache by filepath.Abs(dir), so distinct working
//    directories no longer collide.
//  - selfconfig.ClearCache clears ALL entries, not just the "." entry.
//
// Each test below pins a distinct OBSERVABLE that the pre-G06 implementation
// could not satisfy.
// =============================================================================

// chdirAway moves the process to a temp dir guaranteed to NOT contain a
// .confii.yaml, so tests can verify WithWorkingDir actually picks up the
// supplied dir's self-config (not whatever ambient self-config happens to
// live in the test runner's CWD or repo root).
func chdirAway(t *testing.T) {
	t.Helper()
	awayDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(awayDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })
}

// TestWithWorkingDir_ReadsFromSpecifiedDir asserts that confii.New
// invoked with WithWorkingDir(A) reads its self-config from A, even when
// the process CWD is a different directory entirely. Pre-G06 the
// self-config was always sourced from CWD, so this would fail.
func TestWithWorkingDir_ReadsFromSpecifiedDir(t *testing.T) {
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	dirA := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dirA, ".confii.yaml"),
		[]byte("default_environment: from-A\n"), 0644))

	// Process CWD is somewhere else (no .confii.yaml here).
	chdirAway(t)

	cfg, err := confii.New[any](context.Background(), confii.WithWorkingDir(dirA))
	require.NoError(t, err)
	assert.Equal(t, "from-A", cfg.Env(),
		"G06: WithWorkingDir(A) must surface A's default_environment, not CWD's")
}

// TestTwoConfigsDifferentDirs_DoNotShareCache asserts that two New
// calls with distinct WithWorkingDir values get distinct self-configs.
// Pre-G06 the cache key was always "." so the SECOND call would silently
// observe the FIRST caller's settings.
func TestTwoConfigsDifferentDirs_DoNotShareCache(t *testing.T) {
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	dirA := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dirA, ".confii.yaml"),
		[]byte("default_prefix: APP_A\n"), 0644))

	dirB := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dirB, ".confii.yaml"),
		[]byte("default_prefix: APP_B\n"), 0644))

	chdirAway(t)

	t.Setenv("APP_A_HOST", "host-from-A")
	t.Setenv("APP_B_HOST", "host-from-B")

	cfgA, err := confii.New[any](context.Background(), confii.WithWorkingDir(dirA))
	require.NoError(t, err)
	cfgB, err := confii.New[any](context.Background(), confii.WithWorkingDir(dirB))
	require.NoError(t, err)

	hostA, err := cfgA.Get("host")
	require.NoError(t, err)
	hostB, err := cfgB.Get("host")
	require.NoError(t, err)

	assert.Equal(t, "host-from-A", hostA,
		"G06: cfgA must use APP_A prefix from dirA's self-config")
	assert.Equal(t, "host-from-B", hostB,
		"G06: cfgB must use APP_B prefix from dirB's self-config — "+
			"pre-G06 cache collision would surface APP_A here")
}

// TestDefault_StillReadsCWD asserts backward compatibility: when no
// WithWorkingDir is supplied, behavior matches pre-G06 (self-config is
// read from process CWD).
func TestDefault_StillReadsCWD(t *testing.T) {
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"),
		[]byte("default_environment: from-cwd\n"), 0644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)
	assert.Equal(t, "from-cwd", cfg.Env(),
		"G06: with no WithWorkingDir, self-config must still come from CWD")
}

// TestComposerHonorsWorkingDir asserts that `_include: ./other.yaml`
// resolves relative to WithWorkingDir(A), not relative to the process CWD.
// Pre-G06 the composer was always rooted at "." so this test (loading a
// YAML from A that includes ./other.yaml) would fail when CWD != A.
func TestComposerHonorsWorkingDir(t *testing.T) {
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	dirA := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dirA, "other.yaml"),
		[]byte("included_key: included_value\n"), 0644))
	mainPath := filepath.Join(dirA, "main.yaml")
	require.NoError(t, os.WriteFile(mainPath,
		[]byte("base_key: base_value\n_include: ./other.yaml\n"), 0644))

	// Run from a different CWD so a CWD-rooted composer would fail to
	// find ./other.yaml.
	chdirAway(t)

	cfg, err := confii.New[any](context.Background(),
		confii.WithWorkingDir(dirA),
		confii.WithLoaders(loader.NewYAML(mainPath)),
	)
	require.NoError(t, err,
		"G06: composer rooted at WithWorkingDir must resolve ./other.yaml in dirA")

	included, err := cfg.Get("included_key")
	require.NoError(t, err,
		"G06: _include resolved against WithWorkingDir must surface included keys")
	assert.Equal(t, "included_value", included)

	base, err := cfg.Get("base_key")
	require.NoError(t, err)
	assert.Equal(t, "base_value", base)
}

// TestClearCache_ClearsAllEntries asserts that selfconfig.ClearCache
// purges entries for ALL working directories, not just "." (the pre-G06
// behavior). Reading dir A and then dir B populates two cache slots; a
// single ClearCache must invalidate both, so a subsequent Read of either
// dir produces a freshly-allocated *Settings pointer.
func TestClearCache_ClearsAllEntries(t *testing.T) {
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	dirA := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dirA, ".confii.yaml"),
		[]byte("default_environment: A\n"), 0644))

	dirB := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dirB, ".confii.yaml"),
		[]byte("default_environment: B\n"), 0644))

	firstA, err := selfconfig.Read(dirA)
	require.NoError(t, err)
	require.NotNil(t, firstA)
	firstB, err := selfconfig.Read(dirB)
	require.NoError(t, err)
	require.NotNil(t, firstB)

	// Cache hits return the same pointer.
	hitA, err := selfconfig.Read(dirA)
	require.NoError(t, err)
	assert.Same(t, firstA, hitA, "second Read of dirA must hit cache")
	hitB, err := selfconfig.Read(dirB)
	require.NoError(t, err)
	assert.Same(t, firstB, hitB, "second Read of dirB must hit cache")

	// ClearCache must invalidate BOTH, not just one.
	selfconfig.ClearCache()

	freshA, err := selfconfig.Read(dirA)
	require.NoError(t, err)
	require.NotNil(t, freshA)
	freshB, err := selfconfig.Read(dirB)
	require.NoError(t, err)
	require.NotNil(t, freshB)

	assert.NotSame(t, firstA, freshA,
		"G06: ClearCache must purge dirA's entry — fresh Read must allocate new *Settings")
	assert.NotSame(t, firstB, freshB,
		"G06: ClearCache must purge dirB's entry — fresh Read must allocate new *Settings")
	assert.Equal(t, "A", freshA.DefaultEnvironment,
		"fresh read of dirA must still surface dirA's on-disk value")
	assert.Equal(t, "B", freshB.DefaultEnvironment,
		"fresh read of dirB must still surface dirB's on-disk value")
}
