// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/confiify/confii-go/confii/cmd"
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
	root := &cobra.Command{
		Use:     "confii",
		Short:   "Configuration management CLI",
		Long:    "Confii CLI provides tools for loading, validating, exporting, and comparing configurations.",
		Version: executableVersion(),
	}
	root.SetOut(out)
	root.SetErr(errOut)

	root.AddCommand(
		cmd.NewInitCmd(),
		cmd.NewLoadCmd(),
		cmd.NewGetCmd(),
		cmd.NewValidateCmd(),
		cmd.NewExportCmd(),
		cmd.NewDiffCmd(),
		cmd.NewDebugCmd(),
		cmd.NewExplainCmd(),
		cmd.NewPlanCmd(),
		cmd.NewLintCmd(),
		cmd.NewDocsCmd(),
		cmd.NewMigrateCmd(),
	)
	return root
}

func main() {
	root := newRootCommand(os.Stdout, os.Stderr)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
