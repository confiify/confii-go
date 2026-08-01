// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package hook

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileResolverHook(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cert.pem"), []byte("CERT\n"), 0o600))

	h := NewFileResolverHook(dir)

	tests := []struct {
		name  string
		input any
		want  any
	}{
		{"whole value", "${file:cert.pem}", "CERT\n"},
		{"embedded value", "ca=${file:cert.pem}", "ca=CERT\n"},
		{"non-string passthrough", 42, 42},
		{"no placeholder", "plain", "plain"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := h(context.Background(), "key", tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFileResolverHookUsesCurrentDirectoryByDefault(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previous)) })
	require.NoError(t, os.WriteFile("value.txt", []byte("value"), 0o600))

	h := NewFileResolverHook("")
	got, err := h(context.Background(), "key", "${file:value.txt}")
	require.NoError(t, err)
	assert.Equal(t, "value", got)
}

func TestFileResolverHookRejectsEscapingPath(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("nope"), 0o600))

	h := NewFileResolverHook(dir)
	got, err := h(context.Background(), "key", "${file:../secret.txt}")
	require.Error(t, err)
	assert.Equal(t, "${file:../secret.txt}", got)
}

func TestFileResolverHookRejectsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "large.txt"), []byte("toolong"), 0o600))

	h := NewFileResolverHook(dir, WithFileResolverMaxBytes(3))
	got, err := h(context.Background(), "key", "${file:large.txt}")
	require.Error(t, err)
	assert.Equal(t, "${file:large.txt}", got)
}

func TestFileResolverHookRejectsInvalidContextsAndPaths(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "nested"), 0o700))
	h := NewFileResolverHook(dir)

	var nilContext context.Context
	got, err := h(nilContext, "key", "${file:value.txt}")
	require.Error(t, err)
	assert.Equal(t, "${file:value.txt}", got)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	got, err = h(canceled, "key", "${file:value.txt}")
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, "${file:value.txt}", got)

	got, err = h(context.Background(), "key", "${file:   }")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty path")
	assert.Equal(t, "${file:   }", got)

	got, err = h(context.Background(), "key", "${file:bad\x00path}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NUL")
	assert.Equal(t, "${file:bad\x00path}", got)

	got, err = h(context.Background(), "key", "${file:nested}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a regular file")
	assert.Equal(t, "${file:nested}", got)
}

func TestFileResolverHookAllowsSymlinkInsideBase(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "target.txt"), []byte("target"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(dir, "target.txt"), filepath.Join(dir, "link.txt")))

	h := NewFileResolverHook(dir)
	got, err := h(context.Background(), "key", "${file:link.txt}")
	require.NoError(t, err)
	assert.Equal(t, "target", got)
}

func TestFileResolverHookRejectsSymlinkEscapingBase(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "secret-link.txt")))

	h := NewFileResolverHook(dir)
	got, err := h(context.Background(), "key", "${file:secret-link.txt}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes base directory")
	assert.Equal(t, "${file:secret-link.txt}", got)
}

func TestFileResolverHookRejectsMissingBaseAndInvalidMaxBytes(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	h := NewFileResolverHook(missing)
	got, err := h(context.Background(), "key", "${file:value.txt}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve base directory")
	assert.Equal(t, "${file:value.txt}", got)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "value.txt"), []byte("value"), 0o600))
	h = NewFileResolverHook(dir, WithFileResolverMaxBytes(0))
	got, err = h(context.Background(), "key", "${file:value.txt}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max bytes")
	assert.Equal(t, "${file:value.txt}", got)
}

func TestReadResolvedFileReportsBaseAbsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows keeps the current working directory locked")
	}

	previous, err := os.Getwd()
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, os.Chdir(dir))
	require.NoError(t, os.Remove(dir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previous)) })

	_, err = readResolvedFile(context.Background(), ".", "value.txt", 8<<20)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve base directory")
}

func TestReadResolvedFileReportsReadError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows file ACLs do not make chmod 000 a portable read failure")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "unreadable.txt")
	require.NoError(t, os.WriteFile(path, []byte("secret"), 0o600))
	require.NoError(t, os.Chmod(path, 0))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	_, err := readResolvedFile(context.Background(), dir, "unreadable.txt", 8<<20)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read")
}

func TestFileResolverHookStopsAfterFirstFileError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("ok"), 0o600))
	h := NewFileResolverHook(dir)

	got, err := h(context.Background(), "key", "${file:missing.txt} ${file:ok.txt}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve path")
	assert.Equal(t, "${file:missing.txt} ${file:ok.txt}", got)
}

func TestFileReferenceResolverPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resolver := NewFileReferenceResolver(t.TempDir(), 8<<20)

	_, err := resolver(ctx, ResolverRequest{Target: "value.txt"})
	require.ErrorIs(t, err, context.Canceled)
}

func TestReadResolvedFileRejectsMissingTarget(t *testing.T) {
	_, err := readResolvedFile(context.Background(), t.TempDir(), "missing.txt", 8<<20)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve path")
}

func TestPathWithinBaseHandlesRelError(t *testing.T) {
	assert.False(t, pathWithinBase("relative-base", string(filepath.Separator)+"target"))
}
