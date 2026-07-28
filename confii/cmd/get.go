// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

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
		Use:   "get [env] <key>",
		Short: "Get a single configuration value",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(c *cobra.Command, args []string) error {
			env := ""
			key := args[0]
			if len(args) == 2 {
				env, key = args[0], args[1]
			}

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
				_, err = fmt.Fprintln(c.OutOrStdout(), string(data))
			} else {
				_, err = fmt.Fprintln(c.OutOrStdout(), val)
			}
			return err
		},
	}

	cmd.Flags().StringSliceVarP(&loaders, "loader", "l", nil, "Loader spec (type:source)")
	return cmd
}
