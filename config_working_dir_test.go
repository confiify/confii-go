// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/confiify/confii-go/v2/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func chdirAway(t *testing.T) {
	t.Helper()
	awayDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(awayDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })
}

func TestWithWorkingDir_ReadsFromSpecifiedDir(t *testing.T) {
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	dirA := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dirA, ".confii.yaml"),
		[]byte("default_environment: from-A\n"), 0644))

	chdirAway(t)

	cfg, err := confii.NewWithContext[any](context.Background(), confii.WithWorkingDir(dirA))
	require.NoError(t, err)
	assert.Equal(t, "from-A", cfg.Env(),
		":WithWorkingDir(A) must surface A's default_environment, not CWD's")
}

func TestTwoConfigsDifferentDirs_DoNotShareCache(t *testing.T) {
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	dirA := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dirA, ".confii.yaml"),
		[]byte("env_prefix: APP_A\n"), 0644))

	dirB := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dirB, ".confii.yaml"),
		[]byte("env_prefix: APP_B\n"), 0644))

	chdirAway(t)

	t.Setenv("APP_A_HOST", "host-from-A")
	t.Setenv("APP_B_HOST", "host-from-B")

	cfgA, err := confii.NewWithContext[any](context.Background(), confii.WithWorkingDir(dirA))
	require.NoError(t, err)
	cfgB, err := confii.NewWithContext[any](context.Background(), confii.WithWorkingDir(dirB))
	require.NoError(t, err)

	hostA, err := cfgA.Get("host")
	require.NoError(t, err)
	hostB, err := cfgB.Get("host")
	require.NoError(t, err)

	assert.Equal(t, "host-from-A", hostA,
		":cfgA must use APP_A prefix from dirA's self-config")
	assert.Equal(t, "host-from-B", hostB,
		":cfgB must use APP_B prefix from dirB's self-config — "+
			"pre- cache collision would surface APP_A here")
}

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

	cfg, err := confii.NewWithContext[any](context.Background())
	require.NoError(t, err)
	assert.Equal(t, "from-cwd", cfg.Env(),
		":with no WithWorkingDir, self-config must still come from CWD")
}

func TestComposerHonorsWorkingDir(t *testing.T) {
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	dirA := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dirA, "other.yaml"),
		[]byte("included_key: included_value\n"), 0644))
	mainPath := filepath.Join(dirA, "main().yaml")
	require.NoError(t, os.WriteFile(mainPath,
		[]byte("base_key: base_value\n_include: ./other.yaml\n"), 0644))

	chdirAway(t)

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithWorkingDir(dirA),
		confii.WithLoaders(loader.NewYAML(mainPath)),
	)
	require.NoError(t, err,
		":composer rooted at WithWorkingDir must resolve ./other.yaml in dirA")

	included, err := cfg.Get("included_key")
	require.NoError(t, err,
		":_include resolved against WithWorkingDir must surface included keys")
	assert.Equal(t, "included_value", included)

	base, err := cfg.Get("base_key")
	require.NoError(t, err)
	assert.Equal(t, "base_value", base)
}

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

	hitA, err := selfconfig.Read(dirA)
	require.NoError(t, err)
	assert.Equal(t, firstA, hitA, "second Read of dirA must return cached content")
	assert.NotSame(t, firstA, hitA, "cached settings must be detached")
	hitB, err := selfconfig.Read(dirB)
	require.NoError(t, err)
	assert.Equal(t, firstB, hitB, "second Read of dirB must return cached content")
	assert.NotSame(t, firstB, hitB, "cached settings must be detached")

	selfconfig.ClearCache()

	freshA, err := selfconfig.Read(dirA)
	require.NoError(t, err)
	require.NotNil(t, freshA)
	freshB, err := selfconfig.Read(dirB)
	require.NoError(t, err)
	require.NotNil(t, freshB)

	assert.NotSame(t, firstA, freshA,
		":ClearCache must purge dirA's entry — fresh Read must allocate new *Settings")
	assert.NotSame(t, firstB, freshB,
		":ClearCache must purge dirB's entry — fresh Read must allocate new *Settings")
	assert.Equal(t, "A", freshA.DefaultEnvironment,
		"fresh read of dirA must still surface dirA's on-disk value")
	assert.Equal(t, "B", freshB.DefaultEnvironment,
		"fresh read of dirB must still surface dirB's on-disk value")
}
