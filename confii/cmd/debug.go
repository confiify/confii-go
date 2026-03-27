package cmd

import (
	"context"
	"fmt"
	"os"

	confii "github.com/qualitycoe/confii-go"
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

			issues := 0
			data := cfg.ToDict()

			// Check for nil values.
			for _, k := range cfg.Keys() {
				val, _ := cfg.Get(k)
				if val == nil {
					fmt.Printf("WARNING: key %q has nil value\n", k)
					issues++
				}
			}

			// Check for empty config.
			if len(data) == 0 {
				fmt.Println("WARNING: configuration is empty")
				issues++
			}

			if issues == 0 {
				fmt.Println("No issues found.")
			} else {
				fmt.Printf("\n%d issue(s) found.\n", issues)
				if strict {
					os.Exit(1)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&loaders, "loader", "l", nil, "Loader spec (type:source)")
	cmd.Flags().BoolVar(&strict, "strict", false, "Exit with code 1 if issues found")
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

// NewMigrateCmd creates the 'migrate' command.
func NewMigrateCmd() *cobra.Command {
	var output, targetFormat string

	cmd := &cobra.Command{
		Use:   "migrate <source-type> <config-file>",
		Short: "Migrate configuration from other tools",
		Long:  "Supported sources: dotenv, env, dynaconf, hydra, omegaconf",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			sourceType, configFile := args[0], args[1]
			_ = sourceType // future: parse differently per source type

			cfg, err := buildConfig("", []string{"yaml:" + configFile})
			if err != nil {
				// Try as env file.
				cfg, err = buildConfig("", []string{"env_file:" + configFile})
				if err != nil {
					return fmt.Errorf("could not load %s: %w", configFile, err)
				}
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
			fmt.Print(string(data))
			return nil
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file")
	cmd.Flags().StringVar(&targetFormat, "target-format", "yaml", "Target format (yaml, json, toml)")
	return cmd
}
