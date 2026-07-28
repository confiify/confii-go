// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/confiify/confii-go/selfconfig"
	"github.com/spf13/cobra"
)

const selfConfigFilename = ".confii.yaml"

type initOptions struct {
	strategy           string
	environments       []string
	defaultEnvironment string
	envSwitcher        string
	configDir          string
	minimal            bool
	nonInteractive     bool
	dryRun             bool
	force              bool
}

// NewInitCmd creates the 'init' command. The generated file is the same
// embedded, schema-covered template shipped by the selfconfig package, so CLI
// initialization cannot drift from the settings understood by the library.
func NewInitCmd() *cobra.Command {
	opts := initOptions{}

	cmd := &cobra.Command{
		Use:   "init [directory]",
		Short: "Safely initialize Confii in a project",
		Long: "Initialize a project with a complete .confii.yaml and an optional starter " +
			"configuration layout. By default, Confii asks whether environments should use " +
			"separate files or sections in one file. Existing projects and files are detected " +
			"before anything is written.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			initialized, err := inspectInitialization(dir)
			if err != nil {
				return err
			}
			if len(initialized) > 1 {
				return fmt.Errorf("project has multiple Confii self-configuration files (%s); keep exactly one before running init", strings.Join(initialized, ", "))
			}
			if len(initialized) == 1 && !opts.force {
				if _, readErr := selfconfig.Read(dir); readErr != nil {
					return fmt.Errorf("project appears initialized but %s is invalid: %w", initialized[0], readErr)
				}
				_, err = fmt.Fprintf(c.OutOrStdout(), "Already initialized: %s\nNo files changed.\n", initialized[0])
				return err
			}
			if len(initialized) == 1 && filepath.Base(initialized[0]) != selfConfigFilename {
				return fmt.Errorf("project is initialized with %s; --force cannot create a competing %s file", initialized[0], selfConfigFilename)
			}

			layout, err := resolveInitLayout(c, &opts)
			if err != nil {
				return err
			}
			plan, err := buildInitPlan(dir, layout, opts)
			if err != nil {
				return err
			}
			if err := preflightInitPlan(dir, plan, opts.force); err != nil {
				return err
			}

			if opts.dryRun {
				return printInitPlan(c.OutOrStdout(), "Would create", layout, plan)
			}
			if err := writeInitPlan(plan, opts.force); err != nil {
				return err
			}
			if err := printInitPlan(c.OutOrStdout(), "Created", layout, plan); err != nil {
				return err
			}
			return printInitNextSteps(c.OutOrStdout(), layout, dir, opts.defaultEnvironment, opts.envSwitcher, opts.force)
		},
	}

	cmd.Flags().StringVar(&opts.strategy, "strategy", "", "environment layout: named-files or sectioned")
	cmd.Flags().StringSliceVar(&opts.environments, "environments", []string{"development", "production"}, "environments to scaffold")
	cmd.Flags().StringVar(&opts.defaultEnvironment, "default-environment", "development", "environment used when no explicit selection is provided")
	cmd.Flags().StringVar(&opts.envSwitcher, "env-switcher", "APP_ENV", "OS variable used to select the active environment")
	cmd.Flags().StringVar(&opts.configDir, "config-dir", "config", "project-relative directory for starter configuration")
	cmd.Flags().BoolVar(&opts.minimal, "minimal", false, "create only the complete .confii.yaml")
	cmd.Flags().BoolVar(&opts.nonInteractive, "non-interactive", false, "do not prompt; use named-files unless --strategy is set")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "show the initialization plan without writing files")
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "replace files in the selected initialization plan")
	return cmd
}

func inspectInitialization(dir string) ([]string, error) {
	if err := ensureDirectoryLineage(dir); err != nil {
		return nil, fmt.Errorf("inspect Confii initialization at %s: %w", dir, err)
	}
	var found []string
	for _, name := range selfconfig.CandidateFilenames() {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		switch {
		case err == nil:
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("confii self-configuration candidate %s is not a regular file", path)
			}
			found = append(found, path)
		case errors.Is(err, os.ErrNotExist):
			continue
		default:
			return nil, fmt.Errorf("inspect Confii initialization at %s: %w", path, err)
		}
	}
	return found, nil
}

func resolveInitLayout(c *cobra.Command, opts *initOptions) (initLayout, error) {
	if opts.minimal {
		if strings.TrimSpace(opts.strategy) != "" {
			return "", errors.New("--minimal cannot be combined with --strategy")
		}
		return initLayoutMinimal, nil
	}
	if strings.TrimSpace(opts.strategy) != "" {
		return parseInitLayout(opts.strategy)
	}
	if !opts.nonInteractive {
		return promptInitLayout(c.InOrStdin(), c.OutOrStdout())
	}
	return initLayoutNamedFiles, nil
}

func promptInitLayout(input io.Reader, output io.Writer) (initLayout, error) {
	const message = "Choose how environments are organized:\n" +
		"  1) Separate files (recommended): config/default.yaml + config/{environment}.yaml\n" +
		"  2) One sectioned file: config/application.yaml\n" +
		"  3) Self-configuration only: .confii.yaml\n" +
		"Selection [1]: "
	if _, err := fmt.Fprint(output, message); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read initialization choice: %w", err)
	}
	switch strings.TrimSpace(line) {
	case "", "1":
		return initLayoutNamedFiles, nil
	case "2":
		return initLayoutSectioned, nil
	case "3":
		return initLayoutMinimal, nil
	default:
		return "", fmt.Errorf("invalid selection %q (choose 1, 2, or 3)", strings.TrimSpace(line))
	}
}

func writeSelfConfig(path string, data []byte, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE
	if force {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}

	file, err := os.OpenFile(path, flags, 0o644) // #nosec G304 -- user-selected project path is the command's intended output.
	if err != nil {
		if !force && errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s already exists; use --force to replace it", path)
		}
		return fmt.Errorf("create %s: %w", path, err)
	}

	return writeAndCloseSelfConfig(file, path, data)
}

func writeAndCloseSelfConfig(file io.WriteCloser, path string, data []byte) error {
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}
