// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Command confii loads, inspects, validates, and manages Confii projects.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/confiify/confii-go/v2/confii/cmd"
	"github.com/spf13/cobra"
)

var version = "dev"

func executableVersion() string {
	buildInfo, ok := debug.ReadBuildInfo()
	return resolveVersion(version, buildInfo, ok)
}

func resolveVersion(linkedVersion string, buildInfo *debug.BuildInfo, buildInfoOK bool) string {
	if linkedVersion != "" && linkedVersion != "dev" {
		return linkedVersion
	}
	if buildInfoOK && buildInfo != nil && buildInfo.Main.Version != "" && buildInfo.Main.Version != "(devel)" {
		return buildInfo.Main.Version
	}
	return "dev"
}

func newRootCommand(out, errOut io.Writer) *cobra.Command {
	return cmd.NewRootCommand(executableVersion(), out, errOut)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root := newRootCommand(os.Stdout, os.Stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
