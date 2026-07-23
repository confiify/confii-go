package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	confii "github.com/confiify/confii-go"
	"github.com/spf13/cobra"
)

// NewDebugCmd creates the 'debug' command.
func NewDebugCmd() *cobra.Command {
	var loaders []string
	var key string
	var exportReport string

	cmd := &cobra.Command{
		Use:   "debug [env]",
		Short: "Show source tracking information",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			env := ""
			if len(args) > 0 {
				env = args[0]
			}

			ll, err := parseLoaders(loaders)
			if err != nil {
				return err
			}

			cfg, err := confii.New[any](context.Background(),
				confii.WithLoaders(ll...),
				confii.WithEnv(env),
				confii.WithDebugMode(true),
			)
			if err != nil {
				return err
			}

			if exportReport != "" {
				if err := cfg.ExportDebugReport(exportReport); err != nil {
					return err
				}
				fmt.Println("Debug report exported to", exportReport)
				return nil
			}

			fmt.Print(cfg.PrintDebugInfo(key))
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&loaders, "loader", "l", nil, "Loader spec (type:source)")
	cmd.Flags().StringVar(&key, "key", "", "Specific key to debug")
	cmd.Flags().StringVar(&exportReport, "export-report", "", "Export debug report to JSON file")
	return cmd
}

// NewExplainCmd creates the 'explain' command.
func NewExplainCmd() *cobra.Command {
	var loaders []string
	var key string

	cmd := &cobra.Command{
		Use:   "explain [env]",
		Short: "Show detailed resolution information for a key",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			env := ""
			if len(args) > 0 {
				env = args[0]
			}
			if key == "" {
				return fmt.Errorf("--key is required")
			}

			ll, err := parseLoaders(loaders)
			if err != nil {
				return err
			}

			cfg, err := confii.New[any](context.Background(),
				confii.WithLoaders(ll...),
				confii.WithEnv(env),
				confii.WithDebugMode(true),
			)
			if err != nil {
				return err
			}

			info := cfg.Explain(key)
			for k, v := range info {
				fmt.Printf("%-18s %v\n", k+":", v)
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&loaders, "loader", "l", nil, "Loader spec (type:source)")
	cmd.Flags().StringVar(&key, "key", "", "Key to explain")
	return cmd
}

// NewLintCmd creates the 'lint' command.
//
// Error contract: in --strict mode this command returns a non-nil
// error from RunE when issues are found, and Cobra/main.go translates
// that into a non-zero exit code. In non-strict mode it always
// returns nil and prints findings.
//
// Why no os.Exit: see NewValidateCmd's rationale. Returning an error
// keeps the command callable from tests and from any future embedder.
func NewLintCmd() *cobra.Command {
	var loaders []string
	var strict bool

	cmd := &cobra.Command{
		Use:   "lint [env]",
		Short: "Check for configuration issues",
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

			out := c.OutOrStdout()
			issues := 0
			data := cfg.ToDict()

			// Check for nil values.
			for _, k := range cfg.Keys() {
				val, _ := cfg.Get(k)
				if val == nil {
					if _, err := fmt.Fprintf(out, "WARNING: key %q has nil value\n", k); err != nil {
						return fmt.Errorf("write lint warning: %w", err)
					}
					issues++
				}
			}

			// Check for empty config.
			if len(data) == 0 {
				if _, err := fmt.Fprintln(out, "WARNING: configuration is empty"); err != nil {
					return fmt.Errorf("write lint warning: %w", err)
				}
				issues++
			}

			if issues == 0 {
				_, err := fmt.Fprintln(out, "No issues found.")
				return err
			}

			if _, err := fmt.Fprintf(out, "\n%d issue(s) found.\n", issues); err != nil {
				return fmt.Errorf("write lint summary: %w", err)
			}
			if strict {
				return fmt.Errorf("lint found %d issue(s)", issues)
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&loaders, "loader", "l", nil, "Loader spec (type:source)")
	cmd.Flags().BoolVar(&strict, "strict", false, "Return a non-zero exit code if issues are found")
	return cmd
}

// NewDocsCmd creates the 'docs' command.
func NewDocsCmd() *cobra.Command {
	var loaders []string
	var format, output string

	cmd := &cobra.Command{
		Use:   "docs [env]",
		Short: "Generate configuration documentation",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			env := ""
			if len(args) > 0 {
				env = args[0]
			}

			ll, err := parseLoaders(loaders)
			if err != nil {
				return err
			}

			cfg, err := confii.New[any](context.Background(),
				confii.WithLoaders(ll...),
				confii.WithEnv(env),
				confii.WithDebugMode(true),
			)
			if err != nil {
				return err
			}

			docs, err := cfg.GenerateDocs(format)
			if err != nil {
				return err
			}

			if output != "" {
				return os.WriteFile(output, []byte(docs), 0644)
			}
			fmt.Print(docs)
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&loaders, "loader", "l", nil, "Loader spec (type:source)")
	cmd.Flags().StringVarP(&format, "format", "f", "markdown", "Output format (markdown, json)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file")
	return cmd
}

// migrateSupportedSources lists the source-type values the migrate
// command knows how to dispatch on.
var migrateSupportedSources = []string{
	"auto", "dotenv", "env", "yaml", "yml", "dynaconf", "hydra", "omegaconf",
}

// loadMigrateSource builds a *confii.Config[any] for the migrate
// command, dispatching on sourceType. An empty sourceType (or "auto")
// preserves the legacy YAML-then-env-file auto-detect behaviour.
func loadMigrateSource(sourceType, configFile string) (*confii.Config[any], error) {
	switch sourceType {
	case "", "auto":
		// Legacy behaviour: try YAML first, fall back to env-file.
		cfg, err := buildConfig("", []string{"yaml:" + configFile})
		if err != nil {
			cfg, err = buildConfig("", []string{"env_file:" + configFile})
			if err != nil {
				return nil, fmt.Errorf("could not auto-detect %s: %w", configFile, err)
			}
		}
		return cfg, nil
	case "dotenv":
		return buildConfig("", []string{"env_file:" + configFile})
	case "env":
		// "env" loaders read process environment; the configFile arg is
		// treated as the variable prefix.
		return buildConfig("", []string{"env:" + configFile})
	case "yaml", "yml":
		return buildConfig("", []string{"yaml:" + configFile})
	case "dynaconf":
		// Dynaconf commonly uses settings.toml, settings.yaml, or JSON.
		// Preserve its environment sections and values; Confii's normal
		// environment resolver can then select the active section.
		ext := strings.ToLower(filepath.Ext(configFile))
		switch ext {
		case ".toml":
			return buildConfig("", []string{"toml:" + configFile})
		case ".json":
			return buildConfig("", []string{"json:" + configFile})
		default:
			return buildConfig("", []string{"yaml:" + configFile})
		}
	case "hydra", "omegaconf":
		// Both tools serialize standalone/materialized configuration as
		// YAML. This imports that data faithfully; resolving Hydra config
		// groups or executing OmegaConf resolvers remains the source tool's
		// responsibility before migration.
		return buildConfig("", []string{"yaml:" + configFile})
	default:
		return nil, fmt.Errorf(
			"migrate: unknown source type %q; supported: %v",
			sourceType, migrateSupportedSources,
		)
	}
}

// NewMigrateCmd creates the 'migrate' command.
//
// Error contract: this command returns a non-nil error from RunE when
// the source type is unknown, and when the underlying load/export fails. It
// does NOT silently fall back from one source type to another; the
// only auto-detect path is the explicit "auto" / empty source type,
// preserved for backwards compatibility.
//
// Dispatch is implemented in loadMigrateSource so the policy is
// directly testable without spinning up the full Cobra command.
func NewMigrateCmd() *cobra.Command {
	var output, targetFormat, sourceTypeFlag string

	cmd := &cobra.Command{
		Use:   "migrate [source-type] <config-file>",
		Short: "Migrate configuration from other tools",
		Long: "Migrate configuration from another tool's config file into a Confii-supported format.\n" +
			"\n" +
			"Supported source types (positional or via --source-type):\n" +
			"  auto      - try YAML first, fall back to .env (legacy default)\n" +
			"  dotenv    - .env file\n" +
			"  env       - process environment variables (config-file is the prefix)\n" +
			"  yaml,yml  - YAML config file\n" +
			"  dynaconf  - Dynaconf YAML, TOML, or JSON settings\n" +
			"  hydra     - materialized Hydra YAML\n" +
			"  omegaconf - materialized OmegaConf YAML\n",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(c *cobra.Command, args []string) error {
			var sourceType, configFile string
			switch len(args) {
			case 1:
				// Positional: just <config-file>; source comes from --source-type
				// (empty means auto-detect).
				sourceType = sourceTypeFlag
				configFile = args[0]
			case 2:
				// Positional: <source-type> <config-file>. The flag, if set,
				// must agree with the positional value to avoid surprises.
				sourceType = args[0]
				configFile = args[1]
				if sourceTypeFlag != "" && sourceTypeFlag != sourceType {
					return fmt.Errorf(
						"migrate: --source-type=%q conflicts with positional source %q",
						sourceTypeFlag, sourceType,
					)
				}
			}

			cfg, err := loadMigrateSource(sourceType, configFile)
			if err != nil {
				return err
			}

			if targetFormat == "" {
				targetFormat = "yaml"
			}

			data, err := cfg.Export(targetFormat)
			if err != nil {
				return err
			}

			if output != "" {
				return os.WriteFile(output, data, 0644)
			}
			_, err = fmt.Fprint(c.OutOrStdout(), string(data))
			return err
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file")
	cmd.Flags().StringVar(&targetFormat, "target-format", "yaml", "Target format (yaml, json, toml)")
	cmd.Flags().StringVar(&sourceTypeFlag, "source-type", "", "Source type (dotenv, env, yaml, dynaconf, hydra, omegaconf, or auto)")
	return cmd
}
