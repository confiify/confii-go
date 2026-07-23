package observe

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// G22 sub-fix: ListVersions used to mutate m.versions under an RLock.
// This test runs ListVersions and SaveVersion concurrently; under
// `go test -race` it must not produce a data-race report.
func TestManager_ListVersions_NoMutationUnderConcurrentRead(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	// Seed a few versions so the readers have something to iterate.
	for i := 0; i < 3; i++ {
		_, err := vm.SaveVersion(map[string]any{"i": i}, nil)
		require.NoError(t, err)
	}

	const writers = 3
	const readers = 5
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(writers + readers)

	for w := 0; w < writers; w++ {
		w := w
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_, _ = vm.SaveVersion(map[string]any{"writer": w, "iter": i}, nil)
			}
		}()
	}
	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				vs := vm.ListVersions()
				if len(vs) > iterations*writers {
					t.Errorf("version count %d exceeds maximum %d", len(vs), iterations*writers)
				}
			}
		}()
	}

	wg.Wait()
}

// G22 sub-fix: SaveVersion previously dropped json.Marshal/Unmarshal errors
// silently. Passing a value that cannot be JSON-encoded (channels are not
// representable in JSON) must now propagate a non-nil error.
func TestManager_SaveVersion_PropagatesMarshalErrors(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	bad := map[string]any{
		"chan": make(chan int),
	}
	v, err := vm.SaveVersion(bad, nil)
	assert.Nil(t, v)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version:")
}

// G22 sub-fix: NewVersionManager with an empty storage path must NOT create
// a `.confii/versions/` directory in the working tree. The test changes
// CWD to a temp directory, exercises the default-construction path, and
// asserts that no `.confii/` directory is left behind.
func TestManager_DefaultStorage_NoSourceTreeArtifacts(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(cwd) }()

	tmp := t.TempDir()
	require.NoError(t, os.Chdir(tmp))

	vm := NewVersionManager("", 0)
	v, err := vm.SaveVersion(map[string]any{"key": "value"}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, v.VersionID)

	// In-memory retrieval still works.
	got := vm.GetVersion(v.VersionID)
	require.NotNil(t, got)
	assert.Equal(t, "value", got.Config["key"])

	// No source-tree artifacts.
	_, statErr := os.Stat(filepath.Join(tmp, ".confii"))
	assert.True(t, os.IsNotExist(statErr),
		"default manager must not create .confii/ in the working tree")
}

// G22 sub-fix: Timestamps used to be one-second-precision and tests allowed
// ties. With nanosecond monotonic timestamps, back-to-back saves must
// produce strictly increasing values.
func TestManager_TimestampOrdering_StrictlyMonotonic(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	const n = 50
	timestamps := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		v, err := vm.SaveVersion(map[string]any{"i": i}, nil)
		require.NoError(t, err)
		timestamps = append(timestamps, v.Timestamp)
	}

	for i := 1; i < len(timestamps); i++ {
		assert.Greater(t, timestamps[i], timestamps[i-1],
			"timestamp at index %d (%v) is not strictly greater than previous (%v)",
			i, timestamps[i], timestamps[i-1])
	}
}
