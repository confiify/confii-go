// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"fmt"
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

type selfConfigRequestStoreFunc func(context.Context, SelfConfigSecretRequest) (any, error)

func (f selfConfigRequestStoreFunc) GetSecret(ctx context.Context, key string) (any, error) {
	return f(ctx, SelfConfigSecretRequest{Key: key})
}

func (f selfConfigRequestStoreFunc) GetSecretRequest(ctx context.Context, request SelfConfigSecretRequest) (any, error) {
	return f(ctx, request)
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
	if got, err := h(ctx, "k", "${secret:first}:${secret:second}"); err != nil || got != "FIRST:SECOND" {
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

func TestSelfConfigNamedProviders_RouteExplicitAndEnvironmentDefault(t *testing.T) {
	var alphaBuilds, betaBuilds atomic.Int32
	var requests []string
	register := func(name string, builds *atomic.Int32) {
		RegisterSelfConfigSecretProvider(name, func(cfg map[string]any) (SelfConfigSecretStore, error) {
			builds.Add(1)
			return selfConfigRequestStoreFunc(func(_ context.Context, request SelfConfigSecretRequest) (any, error) {
				requests = append(requests, name+":"+request.Key+":"+request.Version)
				return map[string]any{"credentials": map[string]any{"password": name + "-secret"}}, nil
			}), nil
		})
		t.Cleanup(func() { selfConfigSecretProviders.Delete(name) })
	}
	register("route-alpha", &alphaBuilds)
	register("route-beta", &betaBuilds)

	h, defaultProvider, providers, err := buildSelfConfigSecretHookForEnvironment(map[string]any{
		"default_provider": "shared",
		"environment_defaults": map[string]any{
			"production": "production",
		},
		"providers": map[string]any{
			"shared":     map[string]any{"type": "route-alpha"},
			"production": map[string]any{"type": "route-beta"},
		},
	}, "production")
	if err != nil {
		t.Fatal(err)
	}
	if defaultProvider != "production" || strings.Join(providers, ",") != "production,shared" {
		t.Fatalf("introspection = default %q providers %v", defaultProvider, providers)
	}
	if alphaBuilds.Load() != 0 || betaBuilds.Load() != 0 {
		t.Fatal("named provider factories must be lazy")
	}

	got, err := h(context.Background(), "database.password", "${secret:database:credentials.password:v2}")
	if err != nil || got != "route-beta-secret" {
		t.Fatalf("environment default resolution = %v, %v", got, err)
	}
	got, err = h(context.Background(), "shared.password", "${secret@shared:database:credentials.password}")
	if err != nil || got != "route-alpha-secret" {
		t.Fatalf("explicit resolution = %v, %v", got, err)
	}
	if alphaBuilds.Load() != 1 || betaBuilds.Load() != 1 {
		t.Fatalf("factory builds = alpha %d beta %d", alphaBuilds.Load(), betaBuilds.Load())
	}
	if strings.Join(requests, ",") != "route-beta:database:v2,route-alpha:database:" {
		t.Fatalf("requests = %v", requests)
	}
}

func TestSelfConfigNamedProviders_ValidationAndRoutingErrors(t *testing.T) {
	validProvider := map[string]any{"type": "dict", "entries": map[string]any{"key": "value"}}
	tests := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"providers-not-map", map[string]any{"providers": "dict"}, "non-empty map"},
		{"mixed-shapes", map[string]any{"provider": "dict", "providers": map[string]any{"one": validProvider}}, "cannot be combined"},
		{"provider-not-map", map[string]any{"providers": map[string]any{"one": "dict"}}, "must be a map"},
		{"invalid-alias", map[string]any{"providers": map[string]any{"bad alias": validProvider}}, "invalid provider alias"},
		{"duplicate-alias", map[string]any{"providers": map[string]any{"ONE": validProvider, "one": validProvider}}, "duplicated"},
		{"invalid-type", map[string]any{"providers": map[string]any{"one": map[string]any{"type": 42}}}, "type` must be"},
		{"unknown-type", map[string]any{"providers": map[string]any{"one": map[string]any{"type": "missing-type"}}}, "unsupported type"},
		{"defaults-not-map", map[string]any{"providers": map[string]any{"one": validProvider}, "environment_defaults": "one"}, "must be a map"},
		{"invalid-default-type", map[string]any{"providers": map[string]any{"one": validProvider}, "default_provider": 42}, "must be a non-empty"},
		{"invalid-default-alias", map[string]any{"providers": map[string]any{"one": validProvider}, "default_provider": "bad alias"}, "invalid default_provider"},
		{"empty-environment-default", map[string]any{"providers": map[string]any{"one": validProvider}, "environment_defaults": map[string]any{"production": ""}}, "non-empty provider alias"},
		{"invalid-environment-default", map[string]any{"providers": map[string]any{"one": validProvider}, "environment_defaults": map[string]any{"production": "bad alias"}}, "invalid provider alias"},
		{"undeclared-environment-default", map[string]any{"providers": map[string]any{"one": validProvider}, "environment_defaults": map[string]any{"development": "two"}}, "not declared"},
		{"missing-default", map[string]any{"providers": map[string]any{"one": validProvider}, "default_provider": "two"}, "not declared"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := buildSelfConfigSecretHookForEnvironment(tt.cfg, "production")
			if !errors.Is(err, ErrConfigLoad) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}

	h, _, _, err := buildSelfConfigSecretHookForEnvironment(map[string]any{
		"providers": map[string]any{"one": validProvider},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h(context.Background(), "key", "${secret:key}"); !errors.Is(err, ErrSecretAccess) || !strings.Contains(err.Error(), "no default_provider") {
		t.Fatalf("unqualified error = %v", err)
	}
	if _, err := h(context.Background(), "key", "${secret@missing:key}"); !errors.Is(err, ErrSecretAccess) || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("unknown alias error = %v", err)
	}

	// Omitting type uses the alias as the provider type.
	h, defaultProvider, providers, err := buildSelfConfigSecretHookForEnvironment(map[string]any{
		"default_provider": "dict",
		"providers": map[string]any{
			"dict": map[string]any{"entries": map[string]any{"key": "inferred"}},
		},
	}, "")
	if err != nil || defaultProvider != "dict" || strings.Join(providers, ",") != "dict" {
		t.Fatalf("inferred provider = %q %v, %v", defaultProvider, providers, err)
	}
	if got, err := h(context.Background(), "key", "${secret:key}"); err != nil || got != "inferred" {
		t.Fatalf("inferred provider resolution = %v, %v", got, err)
	}
}

func TestSelfConfigNamedProviders_LazyFactoryAndPathFailures(t *testing.T) {
	for _, tt := range []struct {
		name    string
		factory SelfConfigSecretProviderFactory
		want    string
	}{
		{"factory-error", func(map[string]any) (SelfConfigSecretStore, error) { return nil, errors.New("build failed") }, "build failed"},
		{"nil-store", func(map[string]any) (SelfConfigSecretStore, error) { return nil, nil }, "nil store"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			providerType := "route-" + tt.name
			RegisterSelfConfigSecretProvider(providerType, tt.factory)
			t.Cleanup(func() { selfConfigSecretProviders.Delete(providerType) })
			h, _, _, err := buildSelfConfigSecretHookForEnvironment(map[string]any{
				"default_provider": "primary",
				"providers":        map[string]any{"primary": map[string]any{"type": providerType}},
			}, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h(context.Background(), "key", "${secret:key}"); !errors.Is(err, ErrSecretAccess) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("lazy factory error = %v", err)
			}
		})
	}

	h := makeSelfConfigSecretHook(selfConfigStoreFunc(func(_ context.Context, key string) (any, error) {
		switch key {
		case "scalar":
			return "value", nil
		default:
			return map[string]any{"present": "value"}, nil
		}
	}))
	if _, err := h(context.Background(), "key", "${secret:scalar:nested}"); !errors.Is(err, ErrSecretValidation) || !strings.Contains(err.Error(), "non-map") {
		t.Fatalf("non-map path error = %v", err)
	}
	if _, err := h(context.Background(), "key", "${secret:mapping:missing}"); !errors.Is(err, ErrSecretValidation) || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing path error = %v", err)
	}
	if _, err := h(context.Background(), "key", "${secret:scalar::v1}"); !errors.Is(err, ErrSecretValidation) || !strings.Contains(err.Error(), "versioned") {
		t.Fatalf("unsupported version error = %v", err)
	}
}

func TestSelfConfigNamedProviders_InitializeOnceUnderConcurrency(t *testing.T) {
	const providerType = "route-concurrent"
	var builds atomic.Int32
	RegisterSelfConfigSecretProvider(providerType, func(map[string]any) (SelfConfigSecretStore, error) {
		builds.Add(1)
		return selfConfigStoreFunc(func(context.Context, string) (any, error) { return "resolved", nil }), nil
	})
	t.Cleanup(func() { selfConfigSecretProviders.Delete(providerType) })

	h, _, _, err := buildSelfConfigSecretHookForEnvironment(map[string]any{
		"default_provider": "primary",
		"providers": map[string]any{
			"primary": map[string]any{"type": providerType},
		},
	}, "production")
	if err != nil {
		t.Fatal(err)
	}

	const workers = 32
	errs := make(chan error, workers)
	for range workers {
		go func() {
			got, err := h(context.Background(), "key", "${secret:key}")
			if err == nil && got != "resolved" {
				err = fmt.Errorf("got %v", got)
			}
			errs <- err
		}()
	}
	for range workers {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if builds.Load() != 1 {
		t.Fatalf("factory built %d times, want 1", builds.Load())
	}
}
