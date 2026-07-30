// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package cmd implements the Confii command-line interface and exposes its
// Cobra command tree for provider-enabled distributions and tests.
package cmd

import (
	"io"

	"github.com/spf13/cobra"
)

// NewRootCommand constructs the complete Confii CLI command tree. Applications
// may reuse it in a provider-enabled operational binary after blank-importing
// the selected loader/cloud and secret/cloud modules. This keeps optional SDKs
// out of the standalone core CLI while giving connection tests the exact same
// registered providers and runtime path as the application.
func NewRootCommand(version string, out, errOut io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:     "confii",
		Short:   "Configuration management CLI",
		Long:    "Confii CLI provides tools for loading, validating, exporting, and comparing configurations.",
		Version: version,
	}
	root.SetOut(out)
	root.SetErr(errOut)
	root.AddCommand(
		NewInitCmd(), NewEnvCmd(), NewConnectionsCmd(), NewLoadCmd(), NewGetCmd(),
		NewValidateCmd(), NewExportCmd(), NewDiffCmd(), NewDebugCmd(), NewExplainCmd(),
		NewPlanCmd(), NewLintCmd(), NewDocsCmd(), NewMigrateCmd(),
	)
	return root
}
