// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package sourcetrack

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestG20_FileTracker_NonFileSource_NotChanged(t *testing.T) {
	ft := NewFileTracker()

	require.NoError(t, ft.Track("https://config.example.com/app.yaml"))

	assert.False(t, ft.HasChanged("https://config.example.com/app.yaml"),
		":HTTP URL source must report HasChanged=false")
	assert.False(t, ft.IsTrackable("https://config.example.com/app.yaml"),
		":HTTP URL source must be classified as not trackable")
}

func TestG20_FileTracker_EnvPrefixSource_NotChanged(t *testing.T) {
	ft := NewFileTracker()

	require.NoError(t, ft.Track("environment:APP"))

	assert.False(t, ft.HasChanged("environment:APP"),
		":environment:marker source must report HasChanged=false")
	assert.False(t, ft.IsTrackable("environment:APP"),
		":environment:marker source must be untrackable")
}

func TestG20_FileTracker_UnreadableFile_NotChanged(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "vanish.yaml")
	require.NoError(t, os.WriteFile(f, []byte("k: v\n"), 0o600))

	ft := NewFileTracker()
	require.NoError(t, ft.Track(f))
	require.True(t, ft.IsTrackable(f), "freshly-tracked file must be trackable")

	require.NoError(t, os.Remove(f))

	assert.False(t, ft.HasChanged(f),
		":unreadable file must report HasChanged=false (not true)")
	assert.False(t, ft.IsTrackable(f),
		":unreadable file must be classified as untrackable")

	for i := 0; i < 5; i++ {
		assert.False(t, ft.HasChanged(f),
			":idempotent:HasChanged on untrackable source stays false (call %d)", i)
	}
}

func TestG20_FileTracker_RealFile_StillReportsChanges(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "live.yaml")
	require.NoError(t, os.WriteFile(f, []byte("v: 1\n"), 0o600))

	ft := NewFileTracker()
	require.NoError(t, ft.Track(f))

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, os.WriteFile(f, []byte("v: 2\n"), 0o600))

	assert.True(t, ft.HasChanged(f),
		"a modified file must report changed=true")
}

func TestG20_FileTracker_RealFile_NoChange_ReportsFalse(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "stable.yaml")
	require.NoError(t, os.WriteFile(f, []byte("a: b\n"), 0o600))

	ft := NewFileTracker()
	require.NoError(t, ft.Track(f))

	assert.False(t, ft.HasChanged(f),
		"unmodified real file must report HasChanged=false")

	for i := 0; i < 3; i++ {
		assert.False(t, ft.HasChanged(f),
			"unmodified real file must report HasChanged=false on poll %d", i)
	}
}

func TestG20_FileTracker_UntrackableSet_Idempotent(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "ghost.yaml")
	require.NoError(t, os.WriteFile(f, []byte("ok\n"), 0o600))

	var statCalls int64
	orig := osStat
	osStat = func(name string) (os.FileInfo, error) {
		if name == f {
			atomic.AddInt64(&statCalls, 1)
		}
		return orig(name)
	}
	t.Cleanup(func() { osStat = orig })

	ft := NewFileTracker()
	require.NoError(t, ft.Track(f))

	require.NoError(t, os.Remove(f))

	for i := 0; i < 10; i++ {
		assert.False(t, ft.HasChanged(f),
			"call %d must report HasChanged=false on untrackable source", i)
	}

	calls := atomic.LoadInt64(&statCalls)
	assert.LessOrEqual(t, calls, int64(2),
		" idempotency:os.Stat must be called at most twice "+
			"(initial Track + first untrackable classification); got %d", calls)
	assert.GreaterOrEqual(t, calls, int64(2),
		":at least two stats expected (Track success + first HasChanged failure)")
}

func TestG20_FileTracker_ScheduleRecovery(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "recover().yaml")
	require.NoError(t, os.WriteFile(f, []byte("v: 1\n"), 0o600))

	ft := NewFileTracker()
	require.NoError(t, ft.Track(f))
	require.NoError(t, os.Remove(f))

	require.False(t, ft.HasChanged(f))
	require.False(t, ft.IsTrackable(f))

	require.NoError(t, os.WriteFile(f, []byte("v: 2\n"), 0o600))
	require.NoError(t, ft.Track(f))
	assert.True(t, ft.IsTrackable(f),
		"successful re-Track must clear untrackable classification")
	assert.False(t, ft.HasChanged(f),
		"freshly re-tracked file must report HasChanged=false at steady state")
}

func TestG20_FileTracker_NonFileSource_HasChangedClassifiesLazily(t *testing.T) {
	ft := NewFileTracker()
	src := "s3://bucket/key.yaml"

	assert.False(t, ft.HasChanged(src),
		":HasChanged on a never-tracked s3:// source must classify it untrackable and return false")
	assert.False(t, ft.IsTrackable(src),
		":lazy classification must mark the source untrackable")
}

func TestG20_FileTracker_PermissionDenied_NotChanged(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "locked.yaml")
	require.NoError(t, os.WriteFile(f, []byte("k: v\n"), 0o600))

	ft := NewFileTracker()
	require.NoError(t, ft.Track(f))

	orig := osStat
	osStat = func(name string) (os.FileInfo, error) {
		if name == f {
			return nil, &fs.PathError{Op: "stat", Path: name, Err: os.ErrPermission}
		}
		return orig(name)
	}
	t.Cleanup(func() { osStat = orig })

	require.False(t, ft.HasChanged(f),
		":permission-denied source must report HasChanged=false")
	assert.False(t, ft.IsTrackable(f),
		":permission-denied source must be classified untrackable")

	_, statErr := osStat(f)
	require.True(t, errors.Is(statErr, os.ErrPermission))
}
