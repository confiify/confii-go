// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/confiify/confii-go/v2/selfconfig"
	"go.yaml.in/yaml/v3"
)

type initLayout string

const (
	initLayoutNamedFiles initLayout = "named-files"
	initLayoutSectioned  initLayout = "sectioned"
	initLayoutMinimal    initLayout = "minimal"
)

type initFile struct {
	path string
	data []byte
}

type initFileBackup struct {
	data   []byte
	mode   fs.FileMode
	exists bool
}

func parseInitLayout(value string) (initLayout, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "named-files", "named_files", "separate", "separate-files":
		return initLayoutNamedFiles, nil
	case "sectioned", "single", "single-file":
		return initLayoutSectioned, nil
	default:
		return "", fmt.Errorf("invalid --strategy %q (valid values: named-files, sectioned)", value)
	}
}

func buildInitPlan(root string, layout initLayout, opts initOptions) ([]initFile, error) {
	return buildInitPlanWithRenderer(root, layout, opts, renderInitSelfConfig)
}

func buildInitPlanWithRenderer(
	root string,
	layout initLayout,
	opts initOptions,
	render func(initLayout, string, string, string) ([]byte, error),
) ([]initFile, error) {
	if err := validateInitEnvironment(opts.defaultEnvironment); err != nil {
		return nil, fmt.Errorf("invalid --default-environment: %w", err)
	}
	if strings.TrimSpace(opts.envSwitcher) == "" {
		return nil, errors.New("--env-switcher must not be empty")
	}
	if err := validateEnvSwitcher(opts.envSwitcher); err != nil {
		return nil, fmt.Errorf("invalid --env-switcher: %w", err)
	}
	configDir, err := cleanProjectRelativeDir(opts.configDir)
	if err != nil {
		return nil, err
	}
	environments, err := normalizeInitEnvironments(opts.environments, opts.defaultEnvironment)
	if err != nil {
		return nil, err
	}

	selfConfig, err := render(layout, configDir, opts.defaultEnvironment, opts.envSwitcher)
	if err != nil {
		return nil, err
	}
	format, err := parseInitFormat(defaultString(opts.format, string(initFormatYAML)))
	if err != nil {
		return nil, err
	}
	selfConfig, err = convertInitSelfConfig(selfConfig, format)
	if err != nil {
		return nil, err
	}
	plan := []initFile{{path: filepath.Join(root, selfConfigFilenameFor(format)), data: selfConfig}}
	if layout == initLayoutMinimal {
		return plan, nil
	}

	projectName := initProjectName(root)
	if layout == initLayoutSectioned {
		path := filepath.Join(root, configDir, "application.yaml")
		plan = append(plan, initFile{path: path, data: renderSectionedStarter(projectName, environments)})
		return plan, nil
	}

	plan = append(plan, initFile{
		path: filepath.Join(root, configDir, "default.yaml"),
		data: renderNamedDefaultStarter(projectName),
	})
	for _, environment := range environments {
		plan = append(plan, initFile{
			path: filepath.Join(root, configDir, environment+".yaml"),
			data: renderNamedEnvironmentStarter(environment),
		})
	}
	return plan, nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func convertInitSelfConfig(yamlData []byte, format initFormat) ([]byte, error) {
	if format == initFormatYAML {
		return yamlData, nil
	}
	var values map[string]any
	if err := yaml.Unmarshal(yamlData, &values); err != nil {
		return nil, fmt.Errorf("decode embedded self-config template: %w", err)
	}
	switch format {
	case initFormatJSON:
		data, err := json.MarshalIndent(values, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("render JSON self-config: %w", err)
		}
		return append(data, '\n'), nil
	case initFormatTOML:
		var output bytes.Buffer
		output.WriteString("# Environment overrides may be added as .confii.<environment>.toml.\n")
		if err := toml.NewEncoder(&output).Encode(values); err != nil {
			return nil, fmt.Errorf("render TOML self-config: %w", err)
		}
		return output.Bytes(), nil
	default:
		return nil, fmt.Errorf("unsupported init format %q", format)
	}
}

func initProjectName(root string) string {
	projectName := filepath.Base(filepath.Clean(root))
	if projectName == "." || projectName == string(filepath.Separator) || projectName == "" {
		return "my-service"
	}
	return projectName
}

func validateEnvSwitcher(value string) error {
	for i, r := range value {
		valid := r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '_' || (i > 0 && r >= '0' && r <= '9')
		if !valid {
			return errors.New("use an OS variable name containing letters, digits, and underscores")
		}
	}
	return nil
}

func cleanProjectRelativeDir(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "." {
		return ".", nil
	}
	// Treat both slash styles as separators so a project initialized on one OS
	// cannot accept a path that becomes absolute or escapes the project when
	// the same command is run on another OS.
	normalized := strings.ReplaceAll(value, `\`, "/")
	if filepath.IsAbs(value) || strings.HasPrefix(normalized, "/") || hasWindowsVolumePrefix(normalized) {
		return "", errors.New("--config-dir must be relative to the project root")
	}
	cleaned := pathpkg.Clean(normalized)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("--config-dir must stay within the project root")
	}
	return filepath.FromSlash(cleaned), nil
}

func hasWindowsVolumePrefix(value string) bool {
	return len(value) >= 2 && ((value[0] >= 'A' && value[0] <= 'Z') ||
		(value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':'
}

func normalizeInitEnvironments(values []string, defaultEnvironment string) ([]string, error) {
	seen := make(map[string]bool, len(values)+1)
	result := make([]string, 0, len(values)+1)
	for _, raw := range append([]string{defaultEnvironment}, values...) {
		value := strings.TrimSpace(raw)
		if err := validateInitEnvironment(value); err != nil {
			return nil, fmt.Errorf("invalid environment %q: %w", raw, err)
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result, nil
}

func validateInitEnvironment(value string) error {
	if value == "" {
		return errors.New("must not be empty")
	}
	if value == "default" {
		return errors.New(`"default" is reserved for the shared configuration layer`)
	}
	if value == "." || value == ".." || strings.Contains(value, "..") {
		return errors.New("contains an unsafe path segment")
	}
	for i, r := range value {
		valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || (i > 0 && (r == '-' || r == '_' || r == '.'))
		if !valid {
			return errors.New("use letters, digits, '.', '_' or '-'")
		}
	}
	return nil
}

func renderInitSelfConfig(layout initLayout, configDir, defaultEnvironment, envSwitcher string) ([]byte, error) {
	return renderInitSelfConfigTemplate(string(selfconfig.DefaultYAML()), layout, configDir, defaultEnvironment, envSwitcher)
}

func renderInitSelfConfigTemplate(data string, layout initLayout, configDir, defaultEnvironment, envSwitcher string) ([]byte, error) {
	if layout == initLayoutMinimal {
		return []byte(data), nil
	}
	replacements := map[string]string{
		`default_environment: ""`:    "default_environment: " + strconv.Quote(defaultEnvironment),
		`env_switcher: ""`:           "env_switcher: " + strconv.Quote(envSwitcher),
		"environment_strategy: auto": "environment_strategy: " + strings.ReplaceAll(string(layout), "-", "_"),
		"sources: []":                renderInitSources(layout, configDir),
	}
	for old, replacement := range replacements {
		updated, err := replaceSingleInitSetting(data, old, replacement)
		if err != nil {
			return nil, err
		}
		data = updated
	}
	return []byte(data), nil
}

func replaceSingleInitSetting(data, old, replacement string) (string, error) {
	if strings.Count(data, old) != 1 {
		return "", fmt.Errorf("internal init template drift: expected exactly one %q setting", old)
	}
	return strings.Replace(data, old, replacement, 1), nil
}

func renderInitSources(layout initLayout, configDir string) string {
	configDir = filepath.ToSlash(configDir)
	if layout == initLayoutSectioned {
		return "sources:\n  - type: yaml\n    path: " + strconv.Quote(filepath.ToSlash(filepath.Join(configDir, "application.yaml")))
	}
	return "sources:\n" +
		"  - type: environment_files\n" +
		"    search_paths: [" + strconv.Quote(configDir) + "]\n" +
		"    default_file: default.yaml\n" +
		"    environment_file: \"{environment}.yaml\"\n" +
		"    default_required: true\n" +
		"    environment_required: true"
}

func renderNamedDefaultStarter(projectName string) []byte {
	return []byte("# Shared defaults for every environment.\n" +
		"app:\n  name: " + strconv.Quote(projectName) + "\n" +
		"server:\n  host: 127.0.0.1\n  port: 8080\n" +
		"log:\n  level: info\n")
}

func renderNamedEnvironmentStarter(environment string) []byte {
	var body string
	switch environment {
	case "development", "dev", "local":
		body = "log:\n  level: debug\n"
	case "production", "prod":
		body = "server:\n  host: 0.0.0.0\nlog:\n  level: info\n"
	default:
		body = "# Add only values that differ from config/default.yaml.\n{}\n"
	}
	return []byte("# Overrides for the " + environment + " environment.\n" + body)
}

func renderSectionedStarter(projectName string, environments []string) []byte {
	var builder strings.Builder
	builder.WriteString("# One-file environment model: default is merged with the selected section.\n")
	builder.WriteString("default:\n  app:\n    name: ")
	builder.WriteString(strconv.Quote(projectName))
	builder.WriteString("\n  server:\n    host: 127.0.0.1\n    port: 8080\n  log:\n    level: info\n")
	for _, environment := range environments {
		builder.WriteString(environment)
		builder.WriteString(":\n")
		switch environment {
		case "development", "dev", "local":
			builder.WriteString("  log:\n    level: debug\n")
		case "production", "prod":
			builder.WriteString("  server:\n    host: 0.0.0.0\n  log:\n    level: info\n")
		default:
			builder.WriteString("  {}\n")
		}
	}
	return []byte(builder.String())
}

func preflightInitPlan(root string, plan []initFile, force bool) error {
	var collisions []string
	for _, file := range plan {
		if err := ensureInitTargetInsideProject(root, file.path); err != nil {
			return err
		}
		info, err := os.Lstat(file.path)
		switch {
		case err == nil && !info.Mode().IsRegular():
			return fmt.Errorf("initialization target %s exists and is not a regular file", file.path)
		case err == nil && !force:
			collisions = append(collisions, file.path)
		case errors.Is(err, os.ErrNotExist):
			continue
		case err != nil:
			return fmt.Errorf("inspect initialization target %s: %w", file.path, err)
		}
	}
	if len(collisions) > 0 {
		sort.Strings(collisions)
		return fmt.Errorf("initialization would overwrite existing files (%s); no files changed", strings.Join(collisions, ", "))
	}
	return nil
}

func ensureInitTargetInsideProject(root, target string) error {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("initialization target %s is outside project root %s", target, root)
	}

	current := root
	components := strings.Split(filepath.Dir(relative), string(filepath.Separator))
	for _, component := range components {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		switch {
		case statErr == nil && info.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("initialization target %s traverses symbolic link %s", target, current)
		case statErr == nil && !info.IsDir():
			return fmt.Errorf("initialization target %s has non-directory parent %s", target, current)
		case errors.Is(statErr, os.ErrNotExist):
			return nil
		case statErr != nil:
			return fmt.Errorf("inspect initialization parent %s: %w", current, statErr)
		}
	}
	return nil
}

func writeInitPlan(plan []initFile, force bool) error {
	backups := make(map[string]initFileBackup, len(plan))
	if force {
		for _, file := range plan {
			if err := ensureDirectoryLineage(filepath.Dir(file.path)); err != nil {
				return fmt.Errorf("back up %s before replacement: %w", file.path, err)
			}
			info, err := os.Stat(file.path)
			switch {
			case err == nil:
				data, readErr := os.ReadFile(file.path) // #nosec G304 -- paths were validated as the selected project plan.
				if readErr != nil {
					return fmt.Errorf("back up %s before replacement: %w", file.path, readErr)
				}
				backups[file.path] = initFileBackup{data: data, mode: info.Mode().Perm(), exists: true}
			case errors.Is(err, os.ErrNotExist):
				backups[file.path] = initFileBackup{}
			default:
				return fmt.Errorf("back up %s before replacement: %w", file.path, err)
			}
		}
	}

	written := make([]string, 0, len(plan))
	for _, file := range plan {
		if err := os.MkdirAll(filepath.Dir(file.path), 0o750); err != nil {
			return withInitRollback(fmt.Errorf("create directory for %s: %w", file.path, err), written, backups, force)
		}
		written = append(written, file.path)
		if err := writeSelfConfig(file.path, file.data, force); err != nil {
			return withInitRollback(err, written, backups, force)
		}
	}
	return nil
}

// ensureDirectoryLineage verifies that every existing component on the way to
// dir is a directory. Windows may classify a missing child below a regular
// file as os.ErrNotExist, while Unix reports ENOTDIR at the child. Walking back
// to the nearest existing component gives callers one portable invariant.
func ensureDirectoryLineage(dir string) error {
	current := filepath.Clean(dir)
	for {
		info, err := os.Stat(current)
		switch {
		case err == nil && !info.IsDir():
			return fmt.Errorf("path component %s is not a directory", current)
		case err == nil:
			return nil
		case !errors.Is(err, os.ErrNotExist):
			return err
		}

		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func withInitRollback(cause error, paths []string, backups map[string]initFileBackup, force bool) error {
	var rollbackErrors []error
	for i := len(paths) - 1; i >= 0; i-- {
		path := paths[i]
		backup := backups[path]
		if force && backup.exists {
			if err := os.WriteFile(path, backup.data, backup.mode); err != nil { // #nosec G306 -- restore the original mode.
				rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %s: %w", path, err))
			}
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("remove partial %s: %w", path, err))
		}
	}
	if len(rollbackErrors) > 0 {
		return errors.Join(append([]error{cause, errors.New("initialization rollback was incomplete")}, rollbackErrors...)...)
	}
	return cause
}

func printInitPlan(output io.Writer, verb string, layout initLayout, plan []initFile) error {
	if _, err := fmt.Fprintf(output, "%s Confii project (%s):\n", verb, layout); err != nil {
		return err
	}
	for _, file := range plan {
		if _, err := fmt.Fprintf(output, "  %s\n", file.path); err != nil {
			return err
		}
	}
	return nil
}

func printInitNextSteps(output io.Writer, layout initLayout, root, selfConfigName, environment, envSwitcher string, forced bool) error {
	if forced {
		if _, err := fmt.Fprintln(output, "\nWarning: --force replaced only the files listed above; it never removes files from a previous layout. Review obsolete configuration files manually."); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(output, "\nNext steps:"); err != nil {
		return err
	}
	if cleaned := filepath.Clean(root); cleaned != "." {
		if _, err := fmt.Fprintf(output, "  cd %s\n", strconv.Quote(cleaned)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(output, "  If this is a new Go module: go mod init <module-path>"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, "  go get github.com/confiify/confii-go/v2@latest"); err != nil {
		return err
	}
	if layout == initLayoutMinimal {
		if _, err := fmt.Fprintf(output, "  Edit %s and declare at least one source, then run: confii plan\n", selfConfigName); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintln(output, "  confii env list"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "  %s=%s confii plan\n", envSwitcher, environment); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(output, "  In Go: cfg, err := confii.NewWithContext[YourConfig](ctx)")
	return err
}
