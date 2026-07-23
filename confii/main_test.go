package main

import (
	"bytes"
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
