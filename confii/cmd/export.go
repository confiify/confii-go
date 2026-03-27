package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// NewExportCmd creates the 'export' command.
func NewExportCmd() *cobra.Command {
	var loaders []string
	var format string
	var output string

	cmd := &cobra.Command{
		Use:   "export [env]",
		Short: "Export configuration in a specific format",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			env := ""
			if len(args) > 0 {
				env = args[0]
			}

			cfg, err := buildConfig(env, loaders)
			if err != nil {
				return err
			}

			data, err := cfg.Export(format)
			if err != nil {
				return err
			}

			if output != "" {
				return os.WriteFile(output, data, 0644)
			}
			fmt.Println(string(data))
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&loaders, "loader", "l", nil, "Loader spec (type:source)")
	cmd.Flags().StringVarP(&format, "format", "f", "json", "Output format (json, yaml)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file path")
	return cmd
}
