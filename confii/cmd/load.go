package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// NewLoadCmd creates the 'load' command.
func NewLoadCmd() *cobra.Command {
	var loaders []string

	cmd := &cobra.Command{
		Use:   "load [env]",
		Short: "Load and display merged configuration",
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

			data, err := json.MarshalIndent(cfg.ToDict(), "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&loaders, "loader", "l", nil, "Loader spec (type:source)")
	return cmd
}
