package cmd

import (
	"fmt"
	"os"

	"github.com/confiify/confii-go/validate"
	"github.com/spf13/cobra"
)

// NewValidateCmd creates the 'validate' command.
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
				fmt.Fprintln(os.Stderr, "Validation failed:", err)
				os.Exit(1)
			}

			fmt.Println("Configuration is valid.")
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&loaders, "loader", "l", nil, "Loader spec (type:source)")
	cmd.Flags().StringVar(&schemaFile, "schema", "", "Path to JSON Schema file")
	return cmd
}
