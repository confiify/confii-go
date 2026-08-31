// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"sync/atomic"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/hook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// providerSpy counts every secret resolution attempt. A configuration rejected
// during admission must leave the count at zero: reaching a provider means
// contacting the network with configuration that was never admitted.
type providerSpy struct{ calls atomic.Int64 }

func (s *providerSpy) Hook() hook.Func {
	return func(_ context.Context, _ string, value any) (any, error) {
		str, ok := value.(string)
		if !ok || !containsSecretRef(str) {
			return value, nil
		}
		s.calls.Add(1)
		return "resolved", nil
	}
}

func (s *providerSpy) ClearCache() {}

func containsSecretRef(s string) bool {
	return len(s) > 9 && (s[:9] == "${secret:" || s[:8] == "${secret")
}

type admissionSettings struct {
	Name string `confii:"name"`
	Port int    `confii:"port"`
}

func newAdmissionConfig(t *testing.T, data map[string]any, opts ...confii.Option) (*confii.Config[admissionSettings], *providerSpy, error) {
	t.Helper()
	spy := &providerSpy{}
	all := append([]confii.Option{
		confii.WithLoaders(&g08Loader{source: "base.yaml", data: data}),
		confii.WithSecretResolver(spy),
	}, opts...)
	cfg, err := confii.NewWithContext[admissionSettings](context.Background(), all...)
	return cfg, spy, err
}

// A reference the grammar cannot parse must be refused before anything is
// fetched. Otherwise a typo in one key costs a provider round trip for every
// other key in the file.
func TestAdmission_MalformedReferenceRejectedBeforeAnyProviderCall(t *testing.T) {
	_, spy, err := newAdmissionConfig(t, map[string]any{
		"name": "${secret:}",
		"port": "${secret:db/port}",
	})

	require.Error(t, err, "a malformed reference must fail construction")
	assert.Zero(t, spy.calls.Load(),
		"no provider may be contacted once a reference has failed admission")
}

// Provider routing is not admitted here, deliberately. When the caller supplies
// a resolver, routing is that resolver's business: a custom ManagedSecretResolver
// may implement its own registry, and rejecting an alias Confii does not
// recognize would break it. The bundled single-store resolver reports
// ErrProviderRoutingUnsupported at resolution instead, and a declarative
// configuration validates aliases against the providers it declares.
func TestAdmission_ProviderAliasIsNotJudgedForACallerSuppliedResolver(t *testing.T) {
	_, spy, err := newAdmissionConfig(t, map[string]any{
		"name": "${secret@custom-router:key}",
		"port": 8080,
	})

	require.NoError(t, err,
		"a caller-supplied resolver owns routing; admission must not pre-empt it")
	assert.Equal(t, int64(1), spy.calls.Load(),
		"the reference reaches the resolver, which decides what the alias means")
}

// Sensitivity must be established from the unresolved configuration, so a
// secret-bearing path is classified whatever the resolved value turns out to
// be. Observed through the redacted projection, which is the user-visible
// consequence.
func TestAdmission_SecretBearingPathsAreClassifiedSensitive(t *testing.T) {
	cfg, _, err := newAdmissionConfig(t, map[string]any{
		"name": "${secret:app/name}",
		"port": 8080,
	})
	require.NoError(t, err)

	safe, err := cfg.RedactedDict()
	require.NoError(t, err)
	assert.NotEqual(t, "resolved", safe["name"],
		"a secret-bearing path must be redacted, so it was classified sensitive")
	assert.Equal(t, 8080, safe["port"],
		"a path with no secret must not be classified sensitive")
}

// A valid configuration must still reach its providers; the admission stage
// must not become a blanket refusal.
func TestAdmission_ValidConfigurationStillResolves(t *testing.T) {
	cfg, spy, err := newAdmissionConfig(t, map[string]any{
		"name": "${secret:app/name}",
		"port": 8080,
	})
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, int64(1), spy.calls.Load(),
		"a well-formed reference must reach the provider exactly once")
	typed, err := cfg.TypedCopy()
	require.NoError(t, err)
	assert.Equal(t, "resolved", typed.Name)
}

func TestAdmission_ConfigurationWithoutSecretsNeedsNoProvider(t *testing.T) {
	_, spy, err := newAdmissionConfig(t, map[string]any{"name": "plain", "port": 1})
	require.NoError(t, err)
	assert.Zero(t, spy.calls.Load(),
		"a configuration with no references must not contact a provider")
}

// A failure during admission must publish nothing. The constructor returns an
// error and no Config, so there is no partially built object to observe.
func TestAdmission_FailurePublishesNoConfiguration(t *testing.T) {
	cfg, _, err := newAdmissionConfig(t, map[string]any{
		"name": "${secret:}",
	})
	require.Error(t, err)
	assert.Nil(t, cfg, "a rejected configuration must not be published")
}

// The guarantee must hold for every path that materializes, not only for
// construction. A reload carrying a malformed reference must be refused before
// the provider is contacted, and must leave the previous configuration intact.
func TestAdmission_ReloadIsAdmittedBeforeAnyProviderCall(t *testing.T) {
	loader := &g08Loader{source: "base.yaml", data: map[string]any{
		"name": "initial",
		"port": 8080,
	}}
	spy := &providerSpy{}
	cfg, err := confii.NewWithContext[admissionSettings](context.Background(),
		confii.WithLoaders(loader),
		confii.WithSecretResolver(spy),
	)
	require.NoError(t, err)
	require.Zero(t, spy.calls.Load())

	// The next load carries a malformed reference alongside a valid one.
	loader.data = map[string]any{
		"name": "${secret:}",
		"port": 9090,
	}

	require.Error(t, cfg.Reload(), "a malformed reference must fail the reload")
	assert.Zero(t, spy.calls.Load(),
		"no provider may be contacted for a reload that failed admission")

	typed, err := cfg.TypedCopy()
	require.NoError(t, err)
	assert.Equal(t, "initial", typed.Name,
		"a failed reload must leave the previous configuration in place")
	assert.Equal(t, 8080, typed.Port)
}
