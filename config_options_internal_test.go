// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"testing"

	"github.com/confiify/confii-go/v2/configmap"
	"github.com/stretchr/testify/require"
)

func TestWithEnvPrefix_EmptyAndNilLoaderDedupPaths(t *testing.T) {
	opts := defaultOptions()
	WithEnvPrefix("")(&opts)
	if len(opts.Loaders) != 0 {
		t.Fatalf("empty prefix added %d loaders", len(opts.Loaders))
	}

	opts = defaultOptions()
	opts.Loaders = []Loader{nil}
	WithEnvPrefix("coverage")(&opts)
	if len(opts.Loaders) != 2 || opts.Loaders[1].Source() != "environment:COVERAGE" {
		t.Fatalf("loader chain = %#v", opts.Loaders)
	}
}

func TestEnvPrefixAutoLoader_NoMatches(t *testing.T) {
	l := &envPrefixAutoLoader{prefix: "CONFII_COVERAGE_PREFIX_THAT_DOES_NOT_EXIST"}
	got, err := l.Load(context.Background())
	if err != nil || got != nil {
		t.Fatalf("Load = %#v, %v; want nil, nil", got, err)
	}
}

func TestEnvPrefixAutoLoaderRejectsScalarPathConflict(t *testing.T) {
	t.Setenv("CONFII_PATH_CONFLICT_PARENT", "scalar")
	t.Setenv("CONFII_PATH_CONFLICT_PARENT__CHILD", "nested")
	_, err := (&envPrefixAutoLoader{prefix: "CONFII_PATH_CONFLICT"}).Load(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, err, ErrConfigLoad)
	require.True(t, errors.Is(err, configmap.ErrPathConflict))
}
