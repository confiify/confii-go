// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/selfconfig"
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

			resolvedSchema := schemaFile
			if resolvedSchema == "" {
				settings, err := selfconfig.Read(".")
				if err != nil {
					return fmt.Errorf("read self-config schema_path: %w", err)
				}
				if settings != nil {
					resolvedSchema = settings.SchemaPath
				}
				if resolvedSchema == "" {
					return fmt.Errorf("--schema flag or self-config schema_path is required")
				}
			}

			v, err := validate.NewJSONSchemaValidatorFromFile(resolvedSchema)
			if err != nil {
				return fmt.Errorf("load schema: %w", err)
			}

			// Supplying the resolved path explicitly also prevents a stale or
			// malformed self-config schema_path from outranking --schema while
			// the Config itself is constructed.
			cfg, err := buildConfigWithOptions(env, loaders, confii.WithSchemaPath(resolvedSchema))
			if err != nil {
				return err
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
