// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

func TestRootCommandVersion(t *testing.T) {
	oldVersion := version
	version = "v1.2.3-test"
	t.Cleanup(func() { version = oldVersion })

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := newRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"--version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v (stderr: %s)", err, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "v1.2.3-test") {
		t.Fatalf("version output %q does not contain the linked version", got)
	}
}

func TestRootCommandIncludesEnvironmentManagement(t *testing.T) {
	root := newRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	command, _, err := root.Find([]string{"env", "list"})
	if err != nil {
		t.Fatalf("find env list: %v", err)
	}
	if command.CommandPath() != "confii env list" {
		t.Fatalf("command path = %q, want %q", command.CommandPath(), "confii env list")
	}
}

func TestRootCommandIncludesConnectionTesting(t *testing.T) {
	root := newRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	command, _, err := root.Find([]string{"connections", "test"})
	if err != nil {
		t.Fatalf("find connections test: %v", err)
	}
	if command.CommandPath() != "confii connections test" {
		t.Fatalf("command path = %q, want %q", command.CommandPath(), "confii connections test")
	}
}

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name          string
		linkedVersion string
		buildInfo     *debug.BuildInfo
		buildInfoOK   bool
		want          string
	}{
		{
			name:          "release linker value wins",
			linkedVersion: "v1.3.1",
			buildInfo:     &debug.BuildInfo{Main: debug.Module{Version: "v1.3.0"}},
			buildInfoOK:   true,
			want:          "v1.3.1",
		},
		{
			name:          "go install module version",
			linkedVersion: "dev",
			buildInfo:     &debug.BuildInfo{Main: debug.Module{Version: "v1.3.1"}},
			buildInfoOK:   true,
			want:          "v1.3.1",
		},
		{
			name:          "empty linker value uses module version",
			linkedVersion: "",
			buildInfo:     &debug.BuildInfo{Main: debug.Module{Version: "v1.3.1"}},
			buildInfoOK:   true,
			want:          "v1.3.1",
		},
		{
			name:          "local build remains dev",
			linkedVersion: "dev",
			buildInfo:     &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			buildInfoOK:   true,
			want:          "dev",
		},
		{
			name:          "missing module version remains dev",
			linkedVersion: "dev",
			buildInfo:     &debug.BuildInfo{},
			buildInfoOK:   true,
			want:          "dev",
		},
		{
			name:          "unavailable build info remains dev",
			linkedVersion: "dev",
			buildInfoOK:   false,
			want:          "dev",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveVersion(test.linkedVersion, test.buildInfo, test.buildInfoOK); got != test.want {
				t.Fatalf("resolveVersion = %q, want %q", got, test.want)
			}
		})
	}
}
