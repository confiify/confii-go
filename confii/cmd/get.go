package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// NewGetCmd creates the 'get' command.
func NewGetCmd() *cobra.Command {
	var loaders []string

	cmd := &cobra.Command{
		Use:   "get <env> <key>",
		Short: "Get a single configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			env, key := args[0], args[1]

			cfg, err := buildConfig(env, loaders)
			if err != nil {
				return err
			}

			val, err := cfg.Get(key)
			if err != nil {
				return err
			}

			// Print maps as indented JSON.
			if m, ok := val.(map[string]any); ok {
				data, _ := json.MarshalIndent(m, "", "  ")
				fmt.Println(string(data))
			} else {
				fmt.Println(val)
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&loaders, "loader", "l", nil, "Loader spec (type:source)")
	return cmd
}
