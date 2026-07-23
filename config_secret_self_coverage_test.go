package confii

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type selfConfigStoreFunc func(context.Context, string) (any, error)

func (f selfConfigStoreFunc) GetSecret(ctx context.Context, key string) (any, error) {
	return f(ctx, key)
}

func TestSelfConfigSecretProvider_RegistrationAndBuildErrors(t *testing.T) {
	RegisterSelfConfigSecretProvider("ignored-nil", nil)
	if _, ok := LookupSelfConfigSecretProvider("ignored-nil"); ok {
		t.Fatal("nil provider factory must not be registered")
	}

	if _, err := buildSelfConfigSecretHook(map[string]any{}); !errors.Is(err, ErrConfigLoad) {
		t.Fatalf("missing provider error = %v", err)
	}
	if _, err := buildSelfConfigSecretHook(map[string]any{"provider": "does-not-exist"}); !errors.Is(err, ErrConfigLoad) || !strings.Contains(err.Error(), "registered:") {
		t.Fatalf("unknown provider error = %v", err)
	}

	const name = "coverage-build-error"
	RegisterSelfConfigSecretProvider(name, func(map[string]any) (SelfConfigSecretStore, error) {
		return nil, errors.New("factory failed")
	})
	t.Cleanup(func() { selfConfigSecretProviders.Delete(name) })
	if _, err := buildSelfConfigSecretHook(map[string]any{"provider": name}); !errors.Is(err, ErrConfigLoad) || !strings.Contains(err.Error(), "factory failed") {
		t.Fatalf("factory error = %v", err)
	}
}

func TestRegisteredSelfConfigProviderNames_EmptyAndSorted(t *testing.T) {
	var saved []struct {
		key   any
		value any
	}
	selfConfigSecretProviders.Range(func(key, value any) bool {
		saved = append(saved, struct {
			key   any
			value any
		}{key, value})
		selfConfigSecretProviders.Delete(key)
		return true
	})
	t.Cleanup(func() {
		for _, entry := range saved {
			selfConfigSecretProviders.Store(entry.key, entry.value)
		}
	})

	if got := registeredSelfConfigProviderNames(); got != "(none)" {
		t.Fatalf("empty registry names = %q", got)
	}
	selfConfigSecretProviders.Store(42, "ignored non-string key")
	RegisterSelfConfigSecretProvider("zeta", func(map[string]any) (SelfConfigSecretStore, error) { return nil, nil })
	RegisterSelfConfigSecretProvider("alpha", func(map[string]any) (SelfConfigSecretStore, error) { return nil, nil })
	if got := registeredSelfConfigProviderNames(); got != "alpha, zeta" {
		t.Fatalf("sorted registry names = %q", got)
	}
}

func TestSelfConfigEnvStore_OptionsAndMissingValue(t *testing.T) {
	t.Setenv("PRE_raw.key_SUF", "exact")
	exact := newSelfConfigEnvStore(map[string]any{
		"prefix": "PRE_", "suffix": "_SUF", "transform_key": false,
	})
	got, err := exact.GetSecret(context.Background(), "raw.key")
	if err != nil || got != "exact" {
		t.Fatalf("exact env lookup = %v, %v", got, err)
	}

	transformed := newSelfConfigEnvStore(nil)
	if _, err := transformed.GetSecret(context.Background(), "missing/key-name"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("missing env error = %v", err)
	}
}

func TestSelfConfigFileStore_ReadAndOpenErrors(t *testing.T) {
	base := t.TempDir()
	directoryPath := filepath.Join(base, "directory")
	if err := os.Mkdir(directoryPath, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := newSelfConfigFileStore(map[string]any{"base_dir": base})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSecret(context.Background(), "directory"); err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("directory read error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(base, "plain"), []byte("  value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSecret(context.Background(), "plain")
	if err != nil || got != "value" {
		t.Fatalf("trimmed file secret = %q, %v", got, err)
	}
}

func TestSelfConfigSecretHook_AllValueAndFailurePaths(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request")
	var calls atomic.Int32
	boom := errors.New("secret backend failed")
	store := selfConfigStoreFunc(func(gotCtx context.Context, key string) (any, error) {
		calls.Add(1)
		if gotCtx.Value(contextKey{}) != "request" {
			t.Fatal("context was not propagated")
		}
		if key == "bad" {
			return nil, boom
		}
		return strings.ToUpper(key), nil
	})
	h := makeSelfConfigSecretHook(store)

	if got, err := h(ctx, "k", 42); err != nil || got != 42 {
		t.Fatalf("non-string = %v, %v", got, err)
	}
	if got, err := h(ctx, "k", "plain"); err != nil || got != "plain" {
		t.Fatalf("plain string = %v, %v", got, err)
	}
	if got, err := h(ctx, "k", "${secret:first}:${secret:second:path:v1}"); err != nil || got != "FIRST:SECOND" {
		t.Fatalf("resolved string = %v, %v", got, err)
	}
	beforeFailure := calls.Load()
	original := "${secret:bad}-${secret:not-called}"
	if got, err := h(ctx, "k", original); !errors.Is(err, boom) || got != original {
		t.Fatalf("failed resolution = %v, %v", got, err)
	}
	if delta := calls.Load() - beforeFailure; delta != 1 {
		t.Fatalf("fail-fast store calls = %d, want 1", delta)
	}
}
