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

// G20 Wave 20 — FileTracker source-capability model.
//
// Each test pins a distinct OBSERVABLE behavior that the pre-G20
// implementation would have failed:
//   - non-file scheme sources no longer report changed=true forever
//   - unreadable file sources are demoted to untrackable rather than
//     re-stat'ing on every gate check
//   - real file sources still honor the Wave 7 G14 incremental contract
//   - the untrackable set is idempotent (bounded os.Stat calls)

// TestG20_FileTracker_NonFileSource_NotChanged: an HTTP URL source
// registered with Track must report HasChanged=false. Pre-fix Track
// returned an os.Stat error and HasChanged on the (then-untracked) URL
// returned true on every gate check.
func TestG20_FileTracker_NonFileSource_NotChanged(t *testing.T) {
	ft := NewFileTracker()

	// Track must succeed (no error) for non-file schemes under G20.
	require.NoError(t, ft.Track("https://config.example.com/app.yaml"))

	assert.False(t, ft.HasChanged("https://config.example.com/app.yaml"),
		"G20: HTTP URL source must report HasChanged=false")
	assert.False(t, ft.IsTrackable("https://config.example.com/app.yaml"),
		"G20: HTTP URL source must be classified as not trackable")
}

// TestG20_FileTracker_EnvPrefixSource_NotChanged: an environment-marker
// source must be classified as untrackable. Pre-fix the marker would
// fall into HasChanged's "untracked → return true" branch and re-fire
// the loader on every Reload.
func TestG20_FileTracker_EnvPrefixSource_NotChanged(t *testing.T) {
	ft := NewFileTracker()

	require.NoError(t, ft.Track("environment:APP"))

	assert.False(t, ft.HasChanged("environment:APP"),
		"G20: environment: marker source must report HasChanged=false")
	assert.False(t, ft.IsTrackable("environment:APP"),
		"G20: environment: marker source must be untrackable")
}

// TestG20_FileTracker_UnreadableFile_NotChanged: a file path that was
// trackable at Track time but whose os.Stat now fails (deletion, perm
// change) must be promoted to untrackable and report HasChanged=false.
// Pre-fix, HasChanged returned true on every call ("can't read → treat
// as changed"), defeating the incremental gate.
func TestG20_FileTracker_UnreadableFile_NotChanged(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "vanish.yaml")
	require.NoError(t, os.WriteFile(f, []byte("k: v\n"), 0o600))

	ft := NewFileTracker()
	require.NoError(t, ft.Track(f))
	require.True(t, ft.IsTrackable(f), "freshly-tracked file must be trackable")

	// Make the file unreadable by deleting it.
	require.NoError(t, os.Remove(f))

	// First HasChanged call after deletion → classify untrackable, false.
	assert.False(t, ft.HasChanged(f),
		"G20: unreadable file must report HasChanged=false (not true)")
	assert.False(t, ft.IsTrackable(f),
		"G20: unreadable file must be classified as untrackable")

	// Idempotent on subsequent calls.
	for i := 0; i < 5; i++ {
		assert.False(t, ft.HasChanged(f),
			"G20: idempotent: HasChanged on untrackable source stays false (call %d)", i)
	}
}

// TestG20_FileTracker_RealFile_StillReportsChanges pins the Wave 7 G14
// incremental Reload contract: a real file that genuinely changed on
// disk must still report HasChanged=true. The G20 capability gate must
// not over-suppress.
func TestG20_FileTracker_RealFile_StillReportsChanges(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "live.yaml")
	require.NoError(t, os.WriteFile(f, []byte("v: 1\n"), 0o600))

	ft := NewFileTracker()
	require.NoError(t, ft.Track(f))

	// Mtime granularity on macOS APFS / Linux ext4 is sub-second, but
	// some CI filesystems still round to 1 second. The 50ms sleep
	// matches the existing TestFileTracker_HasChanged_Modified pattern.
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, os.WriteFile(f, []byte("v: 2\n"), 0o600))

	assert.True(t, ft.HasChanged(f),
		"Wave 7 G14: a genuinely modified real file must still report changed=true under G20")
}

// TestG20_FileTracker_RealFile_NoChange_ReportsFalse pins the steady-
// state incremental gate: a tracked, unmodified real file reports
// HasChanged=false on every gate check.
func TestG20_FileTracker_RealFile_NoChange_ReportsFalse(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "stable.yaml")
	require.NoError(t, os.WriteFile(f, []byte("a: b\n"), 0o600))

	ft := NewFileTracker()
	require.NoError(t, ft.Track(f))

	assert.False(t, ft.HasChanged(f),
		"unmodified real file must report HasChanged=false")
	// Repeat to demonstrate the gate stays quiet across N polls.
	for i := 0; i < 3; i++ {
		assert.False(t, ft.HasChanged(f),
			"unmodified real file must report HasChanged=false on poll %d", i)
	}
}

// TestG20_FileTracker_UntrackableSet_Idempotent: registering an
// unreadable source and calling HasChanged 10 times must invoke os.Stat
// at most twice — once during the initial Track attempt (or its
// equivalent first-encounter classification) and at most once more when
// HasChanged first observes the unreadable state and demotes the source
// to untrackable. After demotion every subsequent gate check is a
// pure map lookup.
func TestG20_FileTracker_UntrackableSet_Idempotent(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "ghost.yaml")
	require.NoError(t, os.WriteFile(f, []byte("ok\n"), 0o600))

	// Install a counting os.Stat shim. Restore on test exit.
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
	require.NoError(t, ft.Track(f)) // 1 stat (success)

	// Make the file unreadable.
	require.NoError(t, os.Remove(f))

	// 10 HasChanged calls: only the first should hit os.Stat.
	for i := 0; i < 10; i++ {
		assert.False(t, ft.HasChanged(f),
			"call %d must report HasChanged=false on untrackable source", i)
	}

	calls := atomic.LoadInt64(&statCalls)
	assert.LessOrEqual(t, calls, int64(2),
		"G20 idempotency: os.Stat must be called at most twice "+
			"(initial Track + first untrackable classification); got %d", calls)
	assert.GreaterOrEqual(t, calls, int64(2),
		"G20: at least two stats expected (Track success + first HasChanged failure)")
}

// TestG20_FileTracker_ScheduleRecovery: a Track succeeding again on a
// path that was previously classified untrackable must clear the
// classification. This protects callers that explicitly re-Track after
// a transient failure (e.g. config.go's load loop).
func TestG20_FileTracker_ScheduleRecovery(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "recover.yaml")
	require.NoError(t, os.WriteFile(f, []byte("v: 1\n"), 0o600))

	ft := NewFileTracker()
	require.NoError(t, ft.Track(f))
	require.NoError(t, os.Remove(f))
	// First HasChanged demotes to untrackable.
	require.False(t, ft.HasChanged(f))
	require.False(t, ft.IsTrackable(f))

	// Recreate the file and re-Track.
	require.NoError(t, os.WriteFile(f, []byte("v: 2\n"), 0o600))
	require.NoError(t, ft.Track(f))
	assert.True(t, ft.IsTrackable(f),
		"successful re-Track must clear untrackable classification")
	assert.False(t, ft.HasChanged(f),
		"freshly re-tracked file must report HasChanged=false at steady state")
}

// TestG20_FileTracker_NonFileSource_HasChangedClassifiesLazily: a non-file
// scheme source observed via HasChanged WITHOUT a prior Track call must
// also be classified as untrackable. This exercises the HasChanged-side
// classification fast path (rather than the Track-side one).
func TestG20_FileTracker_NonFileSource_HasChangedClassifiesLazily(t *testing.T) {
	ft := NewFileTracker()
	src := "s3://bucket/key.yaml"

	// No Track call — go straight to HasChanged.
	assert.False(t, ft.HasChanged(src),
		"G20: HasChanged on a never-tracked s3:// source must classify it untrackable and return false")
	assert.False(t, ft.IsTrackable(src),
		"G20: lazy classification must mark the source untrackable")
}

// TestG20_FileTracker_PermissionDenied_NotChanged: parallel to
// TestG20_FileTracker_UnreadableFile_NotChanged but exercises the
// os.ErrPermission branch via an injected stat hook so the test is
// portable to filesystems that don't support 0o000 permission semantics
// (e.g. some sandboxed CI environments).
func TestG20_FileTracker_PermissionDenied_NotChanged(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "locked.yaml")
	require.NoError(t, os.WriteFile(f, []byte("k: v\n"), 0o600))

	ft := NewFileTracker()
	require.NoError(t, ft.Track(f))

	// Inject a permission-denied stat error for f only.
	orig := osStat
	osStat = func(name string) (os.FileInfo, error) {
		if name == f {
			return nil, &fs.PathError{Op: "stat", Path: name, Err: os.ErrPermission}
		}
		return orig(name)
	}
	t.Cleanup(func() { osStat = orig })

	require.False(t, ft.HasChanged(f),
		"G20: permission-denied source must report HasChanged=false")
	assert.False(t, ft.IsTrackable(f),
		"G20: permission-denied source must be classified untrackable")

	// Sanity: assertion above relies on errors.Is plumbing inside
	// readState being a no-op (os.Stat returns the *PathError verbatim,
	// HasChanged only cares err != nil). Pin that assumption explicitly
	// so a future refactor that swaps to errors.Is(...) doesn't quietly
	// regress this branch.
	_, statErr := osStat(f)
	require.True(t, errors.Is(statErr, os.ErrPermission))
}
