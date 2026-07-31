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
	var environment string

	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a single configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			key := args[0]

			cfg, err := buildConfigWithContext(c.Context(), environment, loaders)
			if err != nil {
				return err
			}

			val, err := cfg.GetWithContext(c.Context(), key)
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

	cmd.Flags().StringSliceVarP(&loaders, "loader", "l", nil, loaderSpecHelp)
	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Override the active environment")
	return cmd
}
