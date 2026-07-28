// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/confiify/confii-go/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitCommandCreatesCompleteSelfConfig(t *testing.T) {
	dir := t.TempDir()
	out, err := execCobra(NewInitCmd(), []string{"--minimal", dir})
	require.NoError(t, err)

	path := filepath.Join(dir, selfConfigFilename)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, selfconfig.DefaultYAML(), data)
	assert.Contains(t, out, path)
}

func TestInitCommandCreatesTargetDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "project")
	_, err := execCobra(NewInitCmd(), []string{dir})
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, selfConfigFilename))
}

func TestInitCommandDefaultsToCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(previous) })

	_, err = execCobra(NewInitCmd(), nil)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, selfConfigFilename))
}

func TestInitCommandReportsDirectoryCreationFailure(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(parent, []byte("file"), 0o600))

	_, err := execCobra(NewInitCmd(), []string{filepath.Join(parent, "project")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inspect Confii initialization")
}

func TestInitCommandPreservesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, selfConfigFilename)
	require.NoError(t, os.WriteFile(path, []byte("custom: true\n"), 0o600))

	out, err := execCobra(NewInitCmd(), []string{dir})
	require.NoError(t, err)
	assert.Contains(t, out, "Already initialized")
	assert.Contains(t, out, "No files changed")

	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, "custom: true\n", string(data))
}

func TestInitCommandForceReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, selfConfigFilename)
	require.NoError(t, os.WriteFile(path, []byte("custom: true\n"), 0o600))

	_, err := execCobra(NewInitCmd(), []string{"--force", "--minimal", dir})
	require.NoError(t, err)

	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, selfconfig.DefaultYAML(), data)
}

func TestInitCommandRejectsTooManyDirectories(t *testing.T) {
	_, err := execCobra(NewInitCmd(), []string{"one", "two"})
	require.Error(t, err)
}

func TestInitCommandReportsUnwritableTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, selfConfigFilename)
	require.NoError(t, os.Mkdir(path, 0o700))

	_, err := execCobra(NewInitCmd(), []string{"--force", dir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a regular file")
}

func TestInitCommandRejectsSelfConfigSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on some Windows runners")
	}
	dir := t.TempDir()
	external := filepath.Join(t.TempDir(), "external.yaml")
	require.NoError(t, os.WriteFile(external, []byte("deep_merge: true\n"), 0o600))
	require.NoError(t, os.Symlink(external, filepath.Join(dir, selfConfigFilename)))

	_, err := execCobra(NewInitCmd(), []string{"--force", dir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a regular file")
	assert.Equal(t, "deep_merge: true\n", readTestFile(t, external))
}

func TestInitCommandReturnsOutputWriterError(t *testing.T) {
	dir := t.TempDir()
	cmd := NewInitCmd()
	cmd.SetOut(errorWriter{})
	cmd.SetArgs([]string{"--non-interactive", "--minimal", dir})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

func TestInitCommandReturnsWriteErrorAfterPreflight(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not enforce Unix directory write bits")
	}
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	_, err := execCobra(NewInitCmd(), []string{"--non-interactive", "--minimal", dir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create")
}

func TestWriteAndCloseSelfConfigErrors(t *testing.T) {
	t.Run("write", func(t *testing.T) {
		file := &stubWriteCloser{writeErr: errors.New("disk full")}
		err := writeAndCloseSelfConfig(file, ".confii.yaml", []byte("data"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "write .confii.yaml")
		assert.True(t, file.closed)
	})

	t.Run("close", func(t *testing.T) {
		file := &stubWriteCloser{closeErr: errors.New("close failed")}
		err := writeAndCloseSelfConfig(file, ".confii.yaml", []byte("data"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "close .confii.yaml")
		assert.Equal(t, "data", file.String())
	})
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

type stubWriteCloser struct {
	bytes.Buffer
	writeErr error
	closeErr error
	closed   bool
}

func (f *stubWriteCloser) Write(data []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.Buffer.Write(data)
}

func (f *stubWriteCloser) Close() error {
	f.closed = true
	return f.closeErr
}
