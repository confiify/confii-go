// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"testing"
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
