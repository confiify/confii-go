// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"

	"github.com/confiify/confii-go/validate"
	"github.com/spf13/cobra"
)

// NewValidateCmd creates the 'validate' command.
//
// Error contract: this command returns a non-nil error from RunE when
// validation fails (or when configuration/schema cannot be loaded). The
// caller (main.go / Cobra) is responsible for translating the error
// into a process exit code.
//
// Why no os.Exit: Cobra's RunE exists so commands can return errors and
// let Cobra control exit + error reporting. Calling os.Exit here would
// kill any embedding test process and bypass Cobra's error hooks,
// making the command effectively untestable.
func NewValidateCmd() *cobra.Command {
	var loaders []string
	var schemaFile string

	cmd := &cobra.Command{
		Use:   "validate [env]",
		Short: "Validate configuration against a JSON Schema",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			env := ""
			if len(args) > 0 {
				env = args[0]
			}

			if schemaFile == "" {
				return fmt.Errorf("--schema flag is required")
			}

			cfg, err := buildConfig(env, loaders)
			if err != nil {
				return err
			}

			v, err := validate.NewJSONSchemaValidatorFromFile(schemaFile)
			if err != nil {
				return fmt.Errorf("load schema: %w", err)
			}

			if err := v.Validate(cfg.ToDict()); err != nil {
				return fmt.Errorf("validation failed: %w", err)
			}

			_, err = fmt.Fprintln(c.OutOrStdout(), "Configuration is valid.")
			return err
		},
	}

	cmd.Flags().StringSliceVarP(&loaders, "loader", "l", nil, "Loader spec (type:source)")
	cmd.Flags().StringVar(&schemaFile, "schema", "", "Path to JSON Schema file")
	return cmd
}
