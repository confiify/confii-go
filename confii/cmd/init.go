// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/confiify/confii-go/selfconfig"
	"github.com/spf13/cobra"
)

const selfConfigFilename = ".confii.yaml"

// NewInitCmd creates the 'init' command. The generated file is the same
// embedded, schema-covered template shipped by the selfconfig package, so CLI
// initialization cannot drift from the settings understood by the library.
func NewInitCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init [directory]",
		Short: "Create a complete .confii.yaml self-configuration file",
		Long: "Create a safe-by-default .confii.yaml in a project root. " +
			"The generated file documents every supported self-configuration decision. " +
			"Existing files are preserved unless --force is explicitly supplied.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return fmt.Errorf("create project directory %q: %w", dir, err)
			}

			path := filepath.Join(dir, selfConfigFilename)
			if err := writeSelfConfig(path, selfconfig.DefaultYAML(), force); err != nil {
				return err
			}

			_, err := fmt.Fprintf(c.OutOrStdout(), "Created %s\n", path)
			return err
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "replace an existing .confii.yaml")
	return cmd
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
