package main

import (
	"fmt"
	"io"
	"os"

	"github.com/confiify/confii-go/confii/cmd"
	"github.com/spf13/cobra"
)

var version = "dev"

func newRootCommand(out, errOut io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:     "confii",
		Short:   "Configuration management CLI",
		Long:    "Confii CLI provides tools for loading, validating, exporting, and comparing configurations.",
		Version: version,
	}
	root.SetOut(out)
	root.SetErr(errOut)

	root.AddCommand(
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
