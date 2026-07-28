// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	confii "github.com/confiify/confii-go"
	"github.com/spf13/cobra"
)

type connectionSourceReport struct {
	Order      int    `json:"order"`
	LoaderType string `json:"loader_type"`
	KeyCount   int    `json:"key_count"`
}

type connectionReport struct {
	Status           string                   `json:"status"`
	Environment      string                   `json:"environment,omitempty"`
	Sources          []connectionSourceReport `json:"sources"`
	SecretProvider   string                   `json:"secret_provider,omitempty"`
	SecretReferences int                      `json:"secret_references"`
	KeysChecked      int                      `json:"keys_checked"`
	DurationMS       int64                    `json:"duration_ms"`
}

// NewConnectionsCmd creates the value-safe connection preflight command.
// Optional source and secret providers become available when their modules
// are registered in the containing binary.
func NewConnectionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "connections",
		Aliases: []string{"connection", "connect"},
		Short:   "Test configured source and secret-provider connections",
		Args:    cobra.NoArgs,
	}
	cmd.AddCommand(newConnectionsTestCmd())
	return cmd
}

func newConnectionsTestCmd() *cobra.Command {
	var loaders []string
	var keys []string
	var timeout time.Duration
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "test [env]",
		Short: "Perform real, value-safe reads using the runtime configuration path",
		Long: "Load every configured source with fail-closed error handling, then resolve " +
			"selected or all configuration values with a deadline-aware context. Resolved values, " +
			"source addresses, credentials, and secret identifiers are never printed.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			env := ""
			if len(args) == 1 {
				env = strings.TrimSpace(args[0])
			}
			ctx, cancel := context.WithTimeout(c.Context(), timeout)
			defer cancel()
			started := time.Now()

			cfg, err := buildConfigWithContext(ctx, env, loaders, confii.WithOnError(confii.ErrorPolicyRaise))
			if err != nil {
				return connectionFailure("load configured sources", err)
			}

			checkKeys := cfg.Keys()
			if len(keys) > 0 {
				checkKeys = normalizeConnectionKeys(keys)
				if len(checkKeys) == 0 {
					return fmt.Errorf("--key must contain a non-empty configuration path")
				}
			}
			for _, key := range checkKeys {
				if _, err := cfg.GetCtx(ctx, key); err != nil {
					return connectionFailure("resolve configuration key", err)
				}
			}

			referenceKeys := cfg.SecretReferenceKeys()
			provider := cfg.SecretProvider()
			if provider != "" && countSelectedReferences(referenceKeys, checkKeys) == 0 {
				return fmt.Errorf("connection test could not verify secret provider %q: no selected configuration value contains a secret reference", provider)
			}

			report := connectionReport{
				Status:           "ok",
				Environment:      cfg.SourcePlan().Environment,
				SecretProvider:   provider,
				SecretReferences: countSelectedReferences(referenceKeys, checkKeys),
				KeysChecked:      len(checkKeys),
				DurationMS:       time.Since(started).Milliseconds(),
				Sources:          connectionSources(cfg.Layers()),
			}
			if jsonOutput {
				encoder := json.NewEncoder(c.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(report)
			}
			return printConnectionReport(c, report)
		},
	}
	cmd.Flags().StringSliceVarP(&loaders, "loader", "l", nil, "Loader spec (type:source)")
	cmd.Flags().StringSliceVar(&keys, "key", nil, "Resolve only this configuration key (repeatable)")
	cmd.Flags().DurationVar(&timeout, "timeout", 15*time.Second, "Deadline for context-aware source loads and value reads")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output machine-readable JSON without configuration values")
	return cmd
}

func normalizeConnectionKeys(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key != "" {
			seen[key] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for key := range seen {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func countSelectedReferences(references, selected []string) int {
	selectedSet := make(map[string]struct{}, len(selected))
	for _, key := range selected {
		selectedSet[key] = struct{}{}
	}
	count := 0
	for _, key := range references {
		for selectedKey := range selectedSet {
			if key == selectedKey || strings.HasPrefix(key, selectedKey+".") {
				count++
				break
			}
		}
	}
	return count
}

func connectionSources(layers []map[string]any) []connectionSourceReport {
	reports := make([]connectionSourceReport, 0, len(layers))
	for index, layer := range layers {
		loaderType, _ := layer["loader_type"].(string)
		keyCount, _ := layer["key_count"].(int)
		reports = append(reports, connectionSourceReport{Order: index + 1, LoaderType: loaderType, KeyCount: keyCount})
	}
	return reports
}

func printConnectionReport(c *cobra.Command, report connectionReport) error {
	var output strings.Builder
	output.WriteString("Connection test: OK\n")
	if report.Environment != "" {
		fmt.Fprintf(&output, "Environment: %s\n", report.Environment)
	}
	fmt.Fprintf(&output, "Sources read: %d\n", len(report.Sources))
	for _, source := range report.Sources {
		fmt.Fprintf(&output, "  %d. %s (%d keys)\n", source.Order, source.LoaderType, source.KeyCount)
	}
	if report.SecretProvider != "" {
		fmt.Fprintf(&output, "Secret provider: %s (%d references resolved)\n", report.SecretProvider, report.SecretReferences)
	}
	fmt.Fprintf(&output, "Values checked: %d (contents withheld)\nDuration: %dms\n", report.KeysChecked, report.DurationMS)
	_, err := fmt.Fprint(c.OutOrStdout(), output.String())
	return err
}

func connectionFailure(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &connectionTestError{operation: operation, detail: connectionFailureDetail(err), cause: err}
}

type connectionTestError struct {
	operation string
	detail    string
	cause     error
}

func (e *connectionTestError) Error() string {
	return fmt.Sprintf("connection test failed while attempting to %s: %s", e.operation, e.detail)
}

func (e *connectionTestError) Unwrap() error { return e.cause }

func connectionFailureDetail(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "the configured deadline was exceeded"
	case errors.Is(err, context.Canceled):
		return "the operation was canceled"
	case errors.Is(err, confii.ErrVaultAuth):
		return "provider authentication failed"
	case errors.Is(err, confii.ErrSecretNotFound):
		return "a referenced secret was not found"
	case errors.Is(err, confii.ErrSecretAccess), errors.Is(err, confii.ErrSecretStore):
		return "the secret provider could not complete the read"
	case errors.Is(err, confii.ErrSecretValidation):
		return "the secret provider returned an invalid value"
	case errors.Is(err, confii.ErrConfigNotFound):
		return "a selected configuration key was not found"
	case errors.Is(err, confii.ErrConfigLoad):
		return "a configured source could not be read"
	default:
		return "the provider returned an error (details withheld)"
	}
}
