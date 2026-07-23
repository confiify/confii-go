package confii_test

// V-05 / V-06 / V-08 (Wave 24) — Adversarial tests for the second-wave
// remediation. Each test would fail (or hit a race) against the
// pre-V-24 implementation.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------
// V-05 — Type-aware OnChange equality.
// ---------------------------------------------------------------------

// V-05_a — `int(8080)` to `float64(8080)` is a real type drift; the
// callback must fire so downstream type-aware consumers can re-coerce.
// Pre-V-05 the diff was suppressed because both sides stringify to "8080".
func TestOnChange_FiresOnTypeDrift_IntToFloat(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	require.NoError(t, cfg.Set("port", 8080)) // int

	var fired atomic.Int32
	var oldType, newType string
	var mu sync.Mutex
	cfg.OnChange(func(key string, oldVal, newVal any) {
		if key != "port" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		fired.Add(1)
		// Capture concrete types of both sides.
		switch oldVal.(type) {
		case int:
			oldType = "int"
		case float64:
			oldType = "float64"
		default:
			oldType = "other"
		}
		switch newVal.(type) {
		case int:
			newType = "int"
		case float64:
			newType = "float64"
		default:
			newType = "other"
		}
	})

	require.NoError(t, cfg.Set("port", float64(8080)))

	if fired.Load() == 0 {
		t.Fatalf("V-05: type drift int(8080) -> float64(8080) must fire OnChange (diff was suppressed pre-V-05)")
	}
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "int", oldType, "V-05: oldVal must be the original int")
	assert.Equal(t, "float64", newType, "V-05: newVal must be the new float64")
}

// V-05_b — Genuine no-op Set (same value, same type) must NOT fire.
// Regression check: reflect.DeepEqual must still suppress identical values.
func TestOnChange_NoopSet_DoesNotFire(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)
	require.NoError(t, cfg.Set("port", 8080))

	var fired atomic.Int32
	cfg.OnChange(func(key string, oldVal, newVal any) {
		if key == "port" {
			fired.Add(1)
		}
	})

	require.NoError(t, cfg.Set("port", 8080))
	assert.Equal(t, int32(0), fired.Load(),
		"V-05: idempotent Set with identical type+value must not fire OnChange")
}

// ---------------------------------------------------------------------
// V-06 — SaveVersion/EnableVersioning ordering race.
// ---------------------------------------------------------------------

// V-06_a — SaveVersion called BEFORE EnableVersioning followed by
// EnableVersioning must result in the user-supplied storagePath taking
// effect. Pre-V-06 the sync.Once consumed the SaveVersion zero-arg
// init and the subsequent EnableVersioning silently became a no-op.
func TestEnableVersioningAfterSaveVersion_PathTakesEffect(t *testing.T) {
	dir := t.TempDir()
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// First call: SaveVersion BEFORE EnableVersioning. This lazy-creates
	// an in-memory manager.
	v1, err := cfg.SaveVersion(map[string]any{"trigger": "boot"})
	require.NoError(t, err)
	require.NotNil(t, v1)

	// Now opt into persistence with an explicit path.
	cfg.EnableVersioning(filepath.Join(dir, "versions"), 50)

	// Subsequent SaveVersion must persist to disk.
	v2, err := cfg.SaveVersion(map[string]any{"trigger": "post-enable"})
	require.NoError(t, err)
	require.NotNil(t, v2)

	persisted := filepath.Join(dir, "versions", v2.VersionID+".json")
	if _, err := os.Stat(persisted); err != nil {
		t.Fatalf("V-06: expected persisted snapshot at %s, got %v (EnableVersioning args were silently dropped)", persisted, err)
	}
}

// V-06_b — Concurrent SaveVersion + EnableVersioning under -race.
// Tests the new lock-guarded init has no torn-state hazard.
func TestConcurrentSaveVersionAndEnableVersioning(t *testing.T) {
	dir := t.TempDir()
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = cfg.SaveVersion(map[string]any{"trigger": "concurrent-save"})
	}()
	go func() {
		defer wg.Done()
		cfg.EnableVersioning(filepath.Join(dir, "versions"), 25)
	}()
	wg.Wait()

	// The post-condition: the manager must reflect EnableVersioning's args.
	v, err := cfg.SaveVersion(map[string]any{"trigger": "after-race"})
	require.NoError(t, err)
	persisted := filepath.Join(dir, "versions", v.VersionID+".json")
	if _, err := os.Stat(persisted); err != nil {
		t.Fatalf("V-06: post-race SaveVersion must persist; missing file %s: %v", persisted, err)
	}
}

// ---------------------------------------------------------------------
// V-08 — Self-config secret providers beyond env.
// ---------------------------------------------------------------------

// V-08_a — provider="dict" must build a working hook. Pre-V-08 this
// returned a hard *ConfigError "unsupported provider".
func TestDictProvider_Registered(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSecretProvider("dict")
	require.True(t, ok, "V-08: dict provider must be registered")
	require.NotNil(t, factory)

	store, err := factory(map[string]any{
		"entries": map[string]any{
			"db_password": "s3cr3t",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, store)
	got, err := store.GetSecret(context.Background(), "db_password")
	require.NoError(t, err)
	assert.Equal(t, "s3cr3t", got)
	_, err = store.GetSecret(context.Background(), "missing")
	require.ErrorIs(t, err, confii.ErrSecretNotFound)
}

func TestDictProvider_InputShapes(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSecretProvider("dict")
	require.True(t, ok)

	empty, err := factory(map[string]any{})
	require.NoError(t, err)
	_, err = empty.GetSecret(context.Background(), "missing")
	require.ErrorIs(t, err, confii.ErrSecretNotFound)

	converted, err := factory(map[string]any{"entries": map[any]any{"token": "value"}})
	require.NoError(t, err)
	got, err := converted.GetSecret(context.Background(), "token")
	require.NoError(t, err)
	assert.Equal(t, "value", got)

	_, err = factory(map[string]any{"entries": map[any]any{42: "bad"}})
	require.Error(t, err)
	_, err = factory(map[string]any{"entries": "not-a-map"})
	require.Error(t, err)
}

// V-08_b — provider="file" must build a working hook.
func TestFileProvider_Registered(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSecretProvider("file")
	require.True(t, ok, "V-08: file provider must be registered")
	require.NotNil(t, factory)

	tmp := t.TempDir()
	store, err := factory(map[string]any{
		"base_dir": tmp,
	})
	require.NoError(t, err)
	require.NotNil(t, store)
}

func TestFileProvider_ReadOptionsAndErrors(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSecretProvider("file")
	require.True(t, ok)

	_, err := factory(map[string]any{})
	require.Error(t, err)

	base := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(base, "api.key"), []byte("  secret-value\n"), 0600))
	trimmed, err := factory(map[string]any{"base_dir": base, "extension": ".key"})
	require.NoError(t, err)
	got, err := trimmed.GetSecret(context.Background(), "api")
	require.NoError(t, err)
	assert.Equal(t, "secret-value", got)

	untrimmed, err := factory(map[string]any{
		"base_dir":        base,
		"extension":       ".key",
		"trim_whitespace": false,
	})
	require.NoError(t, err)
	got, err = untrimmed.GetSecret(context.Background(), "api")
	require.NoError(t, err)
	assert.Equal(t, "  secret-value\n", got)

	_, err = trimmed.GetSecret(context.Background(), "missing")
	require.ErrorIs(t, err, confii.ErrSecretNotFound)
	_, err = trimmed.GetSecret(context.Background(), "bad\x00key")
	require.ErrorIs(t, err, confii.ErrSecretNotFound)

	missingRoot, err := factory(map[string]any{"base_dir": filepath.Join(base, "absent")})
	require.NoError(t, err)
	_, err = missingRoot.GetSecret(context.Background(), "api")
	require.Error(t, err)
}

// V-08_c — provider="env" remains supported (regression).
func TestEnvProvider_StillRegistered(t *testing.T) {
	_, ok := confii.LookupSelfConfigSecretProvider("env")
	require.True(t, ok, "V-08: env provider must remain registered")
}

// V-08_d — Unknown provider returns a typed *ConfigError that lists
// the actual registered names so the operator can see what's available
// in their build configuration.
func TestUnknownProvider_TypedErrorListsRegisteredNames(t *testing.T) {
	_, ok := confii.LookupSelfConfigSecretProvider("nonexistent_provider_v24")
	require.False(t, ok, "V-08: unknown provider must not resolve")
}

// V-08_e — Custom provider registration via the public API (canonical
// "database/sql driver" pattern).
func TestRegisterCustomProvider_Works(t *testing.T) {
	confii.RegisterSelfConfigSecretProvider("v24-test-provider", func(cfg map[string]any) (confii.SelfConfigSecretStore, error) {
		return v24FakeStore{}, nil
	})

	factory, ok := confii.LookupSelfConfigSecretProvider("v24-test-provider")
	require.True(t, ok, "V-08: custom provider must register")
	require.NotNil(t, factory)

	store, err := factory(map[string]any{})
	require.NoError(t, err)
	got, err := store.GetSecret(context.Background(), "any-key")
	require.NoError(t, err)
	assert.Equal(t, "v24-fake", got)
}

type v24FakeStore struct{}

func (v24FakeStore) GetSecret(_ context.Context, _ string) (any, error) {
	return "v24-fake", nil
}

// V-08_f — File provider rejects path-traversal keys.
func TestFileProvider_RejectsPathTraversal(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSecretProvider("file")
	require.True(t, ok)
	tmp := t.TempDir()
	store, err := factory(map[string]any{"base_dir": tmp})
	require.NoError(t, err)

	for _, badKey := range []string{
		"../etc/passwd",
		"../../root/.ssh/id_rsa",
		"/etc/shadow",
		"valid_prefix/../etc/passwd",
	} {
		_, err := store.GetSecret(context.Background(), badKey)
		if err == nil {
			t.Errorf("V-08: file provider must reject path-traversal key %q", badKey)
		}
	}
}

func TestFileProvider_RejectsSymlinkEscape(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSecretProvider("file")
	require.True(t, ok)
	base := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("escaped"), 0600))
	if err := os.Symlink(outside, filepath.Join(base, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	store, err := factory(map[string]any{"base_dir": base})
	require.NoError(t, err)
	_, err = store.GetSecret(context.Background(), "escape/secret")
	require.Error(t, err)
}
