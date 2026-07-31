// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/selfconfig"
	"github.com/spf13/cobra"
)

type environmentStatus struct {
	Effective         string                     `json:"effective"`
	ConfiguredDefault string                     `json:"configured_default"`
	SelectedBy        string                     `json:"selected_by"`
	Switcher          string                     `json:"switcher,omitempty"`
	SwitcherValue     string                     `json:"switcher_value,omitempty"`
	Strategy          confii.EnvironmentStrategy `json:"strategy"`
	SelfConfig        string                     `json:"self_config"`
	Available         []string                   `json:"available"`
}

// NewEnvCmd creates the environment inspection and default-management command.
func NewEnvCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Inspect and manage project environments",
		Long: "Inspect the effective environment and the environments discoverable from " +
			"sectioned or named-file configuration. Setting an environment updates the " +
			"project's configured default; an active env_switcher value still has precedence.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			status, err := inspectEnvironmentStatus()
			if err != nil {
				return err
			}
			return printEnvironmentStatus(c, status, jsonOutput)
		},
	}
	cmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output machine-readable JSON")
	cmd.AddCommand(
		newEnvCurrentCmd(&jsonOutput),
		newEnvListCmd(&jsonOutput),
		newEnvSetCmd(),
		newEnvResetCmd(),
	)
	return cmd
}

func newEnvCurrentCmd(jsonOutput *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the effective environment and how it was selected",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			status, err := inspectEnvironmentStatus()
			if err != nil {
				return err
			}
			return printEnvironmentStatus(c, status, *jsonOutput)
		},
	}
}

func newEnvListCmd(jsonOutput *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List environments discoverable from configured sources",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			status, err := inspectEnvironmentStatus()
			if err != nil {
				return err
			}
			if *jsonOutput {
				encoder := json.NewEncoder(c.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(status)
			}
			if len(status.Available) == 0 {
				_, err = fmt.Fprintln(c.OutOrStdout(), "No environments were discovered from configured sources.")
				return err
			}
			for _, name := range status.Available {
				markers := make([]string, 0, 2)
				if name == status.Effective {
					markers = append(markers, "current")
				}
				if name == status.ConfiguredDefault {
					markers = append(markers, "default")
				}
				if len(markers) == 0 {
					if _, err = fmt.Fprintln(c.OutOrStdout(), name); err != nil {
						return err
					}
					continue
				}
				if _, err = fmt.Fprintf(c.OutOrStdout(), "%s (%s)\n", name, strings.Join(markers, ", ")); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func newEnvSetCmd() *cobra.Command {
	var allowUnknown bool
	cmd := &cobra.Command{
		Use:   "set <environment>",
		Short: "Persist the project's default environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if err := validateInitEnvironment(name); err != nil {
				return fmt.Errorf("invalid environment %q: %w", args[0], err)
			}

			path, settings, err := projectSelfConfig()
			if err != nil {
				return err
			}
			if !allowUnknown {
				status, inspectErr := inspectEnvironmentStatus()
				if inspectErr != nil {
					return fmt.Errorf("verify environment before setting it: %w (use --allow-unknown only when creating the source separately)", inspectErr)
				}
				if !containsEnvironment(status.Available, name) {
					return fmt.Errorf("environment %q is not available; choose one from `confii env list` or use --allow-unknown", name)
				}
			}
			if settings.DefaultEnvironment == name {
				_, err = fmt.Fprintf(c.OutOrStdout(), "Default environment is already %s; no files changed.\n", name)
				return err
			}
			if err := updateDefaultEnvironment(path, name); err != nil {
				return err
			}
			selfconfig.ClearCache()
			if _, err := fmt.Fprintf(c.OutOrStdout(), "Default environment set to %s in %s.\n", name, path); err != nil {
				return err
			}
			if value := os.Getenv(settings.EnvSwitcher); settings.EnvSwitcher != "" && value != "" {
				_, err = fmt.Fprintf(c.OutOrStdout(), "Effective environment remains %s while %s is set.\n", value, settings.EnvSwitcher)
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&allowUnknown, "allow-unknown", false, "set a valid name even when no matching source is currently discoverable")
	return cmd
}

func newEnvResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Clear the configured default environment",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			path, settings, err := projectSelfConfig()
			if err != nil {
				return err
			}
			if settings.DefaultEnvironment == "" {
				_, err = fmt.Fprintln(c.OutOrStdout(), "Default environment is already empty; no files changed.")
				return err
			}
			if err := updateDefaultEnvironment(path, ""); err != nil {
				return err
			}
			selfconfig.ClearCache()
			if _, err = fmt.Fprintf(c.OutOrStdout(), "Default environment cleared in %s.\n", path); err != nil {
				return err
			}
			if value := os.Getenv(settings.EnvSwitcher); settings.EnvSwitcher != "" && value != "" {
				_, err = fmt.Fprintf(c.OutOrStdout(), "Effective environment remains %s while %s is set.\n", value, settings.EnvSwitcher)
				return err
			}
			return nil
		},
	}
}

func inspectEnvironmentStatus() (environmentStatus, error) {
	path, settings, err := projectSelfConfig()
	if err != nil {
		return environmentStatus{}, err
	}

	effective := settings.DefaultEnvironment
	selectedBy := "none"
	switcherValue := ""
	if settings.EnvSwitcher != "" {
		switcherValue = os.Getenv(settings.EnvSwitcher)
	}
	if switcherValue != "" {
		effective = switcherValue
		selectedBy = "env_switcher"
	} else if settings.DefaultEnvironment != "" {
		selectedBy = "default_environment"
	}

	// Resolve with the effective name first so sectioned files that omit a
	// default block are still recognized. If a required named override is
	// missing or malformed, retry with an explicit empty environment: inventory
	// must remain usable when a switcher typo is precisely what the user is
	// trying to diagnose.
	cfg, err := buildConfigWithOptions("", nil, confii.WithEnv(effective))
	if err != nil {
		cfg, err = buildConfigWithOptions("", nil, confii.WithEnv(""))
		if err != nil {
			return environmentStatus{}, err
		}
	}
	available, err := cfg.AvailableEnvironments()
	if err != nil {
		return environmentStatus{}, err
	}
	return environmentStatus{
		Effective:         effective,
		ConfiguredDefault: settings.DefaultEnvironment,
		SelectedBy:        selectedBy,
		Switcher:          settings.EnvSwitcher,
		SwitcherValue:     switcherValue,
		Strategy:          cfg.SourcePlan().Strategy,
		SelfConfig:        path,
		Available:         available,
	}, nil
}

func projectSelfConfig() (string, *selfconfig.Settings, error) {
	paths, err := inspectInitialization(".")
	if err != nil {
		return "", nil, err
	}
	if len(paths) == 0 {
		return "", nil, errors.New("project is not initialized; run `confii init` first")
	}
	if len(paths) > 1 {
		return "", nil, fmt.Errorf("project has multiple Confii self-configuration files (%s); keep exactly one", strings.Join(paths, ", "))
	}
	settings, err := selfconfig.Read(".")
	if err != nil {
		return "", nil, err
	}
	if settings == nil {
		return "", nil, errors.New("project self-configuration could not be read")
	}
	return paths[0], settings, nil
}

func printEnvironmentStatus(c *cobra.Command, status environmentStatus, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(c.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(status)
	}
	effective := status.Effective
	if effective == "" {
		effective = "(none)"
	}
	configuredDefault := status.ConfiguredDefault
	if configuredDefault == "" {
		configuredDefault = "(none)"
	}
	switcher := status.Switcher
	if switcher == "" {
		switcher = "(not configured)"
	} else if status.SwitcherValue == "" {
		switcher += " (unset)"
	} else {
		switcher += "=" + status.SwitcherValue
	}
	_, err := fmt.Fprintf(c.OutOrStdout(), "Environment: %s\nSelected by: %s\nConfigured default: %s\nSwitcher: %s\nStrategy: %s\nSelf-config: %s\n",
		effective, status.SelectedBy, configuredDefault, switcher, status.Strategy, status.SelfConfig)
	return err
}

func containsEnvironment(environments []string, name string) bool {
	index := sort.SearchStrings(environments, name)
	return index < len(environments) && environments[index] == name
}

func updateDefaultEnvironment(path, value string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to update non-regular self-config %s", path)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is a validated project self-config candidate.
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	var updated []byte
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		updated, err = replaceTopLevelSetting(data, "default_environment:", strconv.Quote(value))
	case ".toml":
		updated, err = replaceTopLevelSetting(data, "default_environment", strconv.Quote(value))
	case ".json":
		var document map[string]any
		if err = json.Unmarshal(data, &document); err == nil {
			document["default_environment"] = value
			updated, err = json.MarshalIndent(document, "", "  ")
			updated = append(updated, '\n')
		}
	default:
		err = fmt.Errorf("unsupported self-config format %s", filepath.Ext(path))
	}
	if err != nil {
		return fmt.Errorf("update default_environment in %s: %w", path, err)
	}
	return atomicReplaceFile(path, updated, info.Mode().Perm())
}

func replaceTopLevelSetting(data []byte, key, quotedValue string) ([]byte, error) {
	lines := bytes.SplitAfter(data, []byte("\n"))
	matches := 0
	for i, rawLine := range lines {
		line := strings.TrimSuffix(string(rawLine), "\n")
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		isYAML := strings.HasSuffix(key, ":")
		matched := isYAML && strings.HasPrefix(line, key) || !isYAML &&
			(strings.HasPrefix(line, key+" ") || strings.HasPrefix(line, key+"="))
		if !matched {
			continue
		}
		matches++
		ending := ""
		if bytes.HasSuffix(rawLine, []byte("\r\n")) {
			ending = "\r\n"
		} else if bytes.HasSuffix(rawLine, []byte("\n")) {
			ending = "\n"
		}
		comment := topLevelSettingComment(line)
		if isYAML {
			lines[i] = []byte(key + " " + quotedValue + comment + ending)
		} else {
			lines[i] = []byte(key + " = " + quotedValue + comment + ending)
		}
	}
	if matches == 0 {
		ending := "\n"
		if bytes.Contains(data, []byte("\r\n")) {
			ending = "\r\n"
		}
		updated := append([]byte(nil), data...)
		if len(updated) > 0 && !bytes.HasSuffix(updated, []byte("\n")) {
			updated = append(updated, ending...)
		}
		if strings.HasSuffix(key, ":") {
			return append(updated, []byte(key+" "+quotedValue+ending)...), nil
		}
		return append(updated, []byte(key+" = "+quotedValue+ending)...), nil
	}
	if matches > 1 {
		return nil, fmt.Errorf("top-level setting %q occurs more than once", strings.TrimSuffix(key, ":"))
	}
	return bytes.Join(lines, nil), nil
}

func topLevelSettingComment(line string) string {
	var quote rune
	escaped := false
	for index, character := range line {
		if escaped {
			escaped = false
			continue
		}
		if character == '\\' && quote == '"' {
			escaped = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			}
			continue
		}
		if character == '"' || character == '\'' {
			quote = character
			continue
		}
		if character == '#' {
			prefix := line[:index]
			return strings.TrimRight(prefix[len(strings.TrimRight(prefix, " \t")):], "\r") + line[index:]
		}
	}
	return ""
}

func atomicReplaceFile(path string, data []byte, mode fs.FileMode) (returnErr error) {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".confii-env-*")
	if err != nil {
		return fmt.Errorf("create temporary self-config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if returnErr != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("preserve self-config permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary self-config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary self-config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary self-config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		if fallbackErr := replaceFileWithBackup(temporaryPath, path); fallbackErr != nil {
			return fmt.Errorf("replace %s: %w (fallback: %v)", path, err, fallbackErr)
		}
	}
	return nil
}

func replaceFileWithBackup(temporaryPath, path string) error {
	backup, err := os.CreateTemp(filepath.Dir(path), ".confii-env-backup-*")
	if err != nil {
		return err
	}
	backupPath := backup.Name()
	if err := backup.Close(); err != nil {
		return err
	}
	if err := os.Remove(backupPath); err != nil {
		return err
	}
	if err := os.Rename(path, backupPath); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		if rollbackErr := os.Rename(backupPath, path); rollbackErr != nil {
			return fmt.Errorf("install replacement: %w; restore original: %v", err, rollbackErr)
		}
		return err
	}
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("remove replacement backup %s: %w", backupPath, err)
	}
	return nil
}
