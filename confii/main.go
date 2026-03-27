package main

import (
	"fmt"
	"os"

	"github.com/confiify/confii-go/cmd/confii/cmd"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "confii",
		Short: "Configuration management CLI",
		Long:  "Confii CLI provides tools for loading, validating, exporting, and comparing configurations.",
	}

	root.AddCommand(
		cmd.NewLoadCmd(),
		cmd.NewGetCmd(),
		cmd.NewValidateCmd(),
		cmd.NewExportCmd(),
		cmd.NewDiffCmd(),
		cmd.NewDebugCmd(),
		cmd.NewExplainCmd(),
		cmd.NewLintCmd(),
		cmd.NewDocsCmd(),
		cmd.NewMigrateCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
