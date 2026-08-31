// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/hook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// canary is a value that must never reach a redacted projection, an export, an
// error, or any formatted representation.
const canary = "CANARY-a7f3d91e-must-not-appear"

// canaryResolver resolves every secret reference to the canary, so any path
// that carries a resolved secret carries something recognisable.
type canaryResolver struct{}

func (canaryResolver) Hook() hook.Func {
	return func(_ context.Context, _ string, value any) (any, error) {
		s, ok := value.(string)
		if !ok || !strings.Contains(s, "${secret:") {
			return value, nil
		}
		return canary, nil
	}
}

func (canaryResolver) ClearCache() {}

func newRedactionConfig(t *testing.T, data map[string]any, opts ...confii.Option) *confii.Config[any] {
	t.Helper()
	all := append([]confii.Option{
		confii.WithLoaders(&g08Loader{source: "base.yaml", data: data}),
		confii.WithSecretResolver(&canaryResolver{}),
	}, opts...)
	cfg, err := confii.NewWithContext[any](context.Background(), all...)
	require.NoError(t, err)
	return cfg
}

func TestRedactedDict_ReplacesSecretBackedValues(t *testing.T) {
	cfg := newRedactionConfig(t, map[string]any{
		"database": map[string]any{
			"host":     "localhost",
			"password": "${secret:db/password}",
		},
	})

	safe, err := cfg.RedactedDict()
	require.NoError(t, err)

	db, ok := safe["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "localhost", db["host"],
		"a sibling that is not secret-backed must survive redaction")
	assert.NotEqual(t, canary, db["password"])
	assert.NotContains(t, fmt.Sprint(safe), canary,
		"the canary must not appear anywhere in the projection")
}

// A parent of a secret-backed key is not itself secret. Redacting the whole
// subtree would lose values the caller needs and that were never sensitive.
func TestRedactedDict_DoesNotOverRedactParents(t *testing.T) {
	cfg := newRedactionConfig(t, map[string]any{
		"service": map[string]any{
			"name":     "billing",
			"replicas": 3,
			"api_key":  "${secret:svc/key}",
		},
	})

	safe, err := cfg.RedactedDict()
	require.NoError(t, err)

	svc := safe["service"].(map[string]any)
	assert.Equal(t, "billing", svc["name"])
	assert.Equal(t, 3, svc["replicas"])
	assert.NotContains(t, fmt.Sprint(svc["api_key"]), canary)
}

func TestRedactedDict_CoversDeclaredSensitivePaths(t *testing.T) {
	cfg := newRedactionConfig(t,
		map[string]any{"app": map[string]any{"licence": "not-a-secret-reference"}},
		confii.WithSensitivePaths("app.licence"),
	)

	safe, err := cfg.RedactedDict()
	require.NoError(t, err)
	app := safe["app"].(map[string]any)
	assert.NotEqual(t, "not-a-secret-reference", app["licence"],
		"a declared sensitive path must be redacted even without a secret reference")
}

func TestRedactedDict_IsDetachedFromConfig(t *testing.T) {
	cfg := newRedactionConfig(t, map[string]any{
		"database": map[string]any{"host": "localhost"},
	})

	safe, err := cfg.RedactedDict()
	require.NoError(t, err)
	safe["database"].(map[string]any)["host"] = "mutated"

	again, err := cfg.RedactedDict()
	require.NoError(t, err)
	assert.Equal(t, "localhost", again["database"].(map[string]any)["host"],
		"mutating a projection must not affect configuration state")
}

func TestExportRedacted_OmitsSecretValues(t *testing.T) {
	cfg := newRedactionConfig(t, map[string]any{
		"database": map[string]any{
			"host":     "localhost",
			"password": "${secret:db/password}",
		},
	})

	for _, format := range []string{"json", "yaml", "toml"} {
		t.Run(format, func(t *testing.T) {
			data, err := cfg.ExportRedacted(format)
			require.NoError(t, err)
			assert.NotContains(t, string(data), canary,
				"a redacted export must not carry the secret")
			assert.Contains(t, string(data), "localhost",
				"non-secret values must still be exported")
		})
	}
}

func TestExportRedacted_ProducesValidJSON(t *testing.T) {
	cfg := newRedactionConfig(t, map[string]any{
		"database": map[string]any{"password": "${secret:db/password}"},
	})

	data, err := cfg.ExportRedacted("json")
	require.NoError(t, err)

	var round map[string]any
	require.NoError(t, json.Unmarshal(data, &round),
		"a redacted export must still be well-formed")
}

func TestExportRedacted_RejectsUnknownFormat(t *testing.T) {
	cfg := newRedactionConfig(t, map[string]any{"a": 1})
	_, err := cfg.ExportRedacted("not-a-format")
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrConfigInvalid)
}

// The unredacted paths remain available and must keep working; they are simply
// documented as unsafe. This pins that they were not silently changed.
func TestExport_StillCarriesResolvedValues(t *testing.T) {
	cfg := newRedactionConfig(t, map[string]any{
		"database": map[string]any{"password": "${secret:db/password}"},
	})

	data, err := cfg.Export("json")
	require.NoError(t, err)
	assert.Contains(t, string(data), canary,
		"Export is the unredacted path and must not have changed meaning")
}

func TestRedactedDict_SurvivesRefresh(t *testing.T) {
	cfg := newRedactionConfig(t, map[string]any{
		"database": map[string]any{"password": "${secret:db/password}"},
	})

	require.NoError(t, cfg.RefreshSecrets())

	safe, err := cfg.RedactedDict()
	require.NoError(t, err)
	assert.NotContains(t, fmt.Sprint(safe), canary,
		"a refreshed secret must still be redacted")
}

func TestRedactedDict_NoCanaryInFormattedForms(t *testing.T) {
	cfg := newRedactionConfig(t, map[string]any{
		"database": map[string]any{"password": "${secret:db/password}"},
	})

	safe, err := cfg.RedactedDict()
	require.NoError(t, err)

	for name, rendered := range map[string]string{
		"%v":     fmt.Sprintf("%v", safe),
		"%+v":    fmt.Sprintf("%+v", safe),
		"%#v":    fmt.Sprintf("%#v", safe),
		"Sprint": fmt.Sprint(safe),
	} {
		t.Run(name, func(t *testing.T) {
			assert.False(t, strings.Contains(rendered, canary),
				"the canary leaked through %s", name)
		})
	}
}

// A secret inside a slice, and inside a map inside a slice, must be redacted.
//
// This case leaked when the feature first landed: the redaction walk descended
// into maps only, so a map nested in a slice was copied through verbatim. The
// classifier passes a slice's path to its elements without an index, so the
// redaction walk has to do the same or the two disagree about where a secret
// lives.
func TestRedactedDict_RedactsSecretsInsideSlices(t *testing.T) {
	cfg := newRedactionConfig(t, map[string]any{
		"tokens": []any{"${secret:a}", "plain"},
		"nested": []any{map[string]any{"pw": "${secret:b}", "host": "localhost"}},
		"deep":   []any{[]any{map[string]any{"key": "${secret:c}"}}},
	})

	safe, err := cfg.RedactedDict()
	require.NoError(t, err)

	rendered := fmt.Sprint(safe)
	assert.NotContains(t, rendered, canary,
		"no secret may survive in a slice, however deeply nested")

	// A non-secret sibling inside the same nested map still survives.
	nested := safe["nested"].([]any)[0].(map[string]any)
	assert.Equal(t, "localhost", nested["host"],
		"redacting a slice element must not discard its plain siblings")
}
