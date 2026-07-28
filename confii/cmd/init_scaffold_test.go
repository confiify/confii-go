// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitCommandScaffoldsNamedFilesThatLoad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "todo-service")
	out, err := execCobra(NewInitCmd(), []string{
		"--strategy", "named-files",
		"--environments", "development,staging,production",
		dir,
	})
	require.NoError(t, err)

	assert.Contains(t, out, "named-files")
	for _, name := range []string{"default.yaml", "development.yaml", "staging.yaml", "production.yaml"} {
		assert.FileExists(t, filepath.Join(dir, "config", name))
	}
	selfConfig := readTestFile(t, filepath.Join(dir, selfConfigFilename))
	assert.Contains(t, selfConfig, "environment_strategy: named_files")
	assert.Contains(t, selfConfig, "type: environment_files")
	assert.Contains(t, selfConfig, `default_environment: "development"`)

	withWorkingDirectory(t, dir)
	selfconfig.ClearCache()
	cfg, err := confii.New[any](context.Background(), confii.WithEnv("production"))
	require.NoError(t, err)
	assert.Equal(t, "todo-service", getTestString(t, cfg, "app.name"))
	assert.Equal(t, "0.0.0.0", getTestString(t, cfg, "server.host"))
	assert.Equal(t, "info", getTestString(t, cfg, "log.level"))

	// The generated control plane must also support its advertised final
	// environment-variable override layer once a project chooses a prefix.
	updatedSelfConfig := strings.Replace(selfConfig, `env_prefix: ""`, "env_prefix: APP", 1)
	require.NotEqual(t, selfConfig, updatedSelfConfig)
	require.NoError(t, os.WriteFile(filepath.Join(dir, selfConfigFilename), []byte(updatedSelfConfig), 0644))
	t.Setenv("APP_SERVER__PORT", "9090")
	selfconfig.ClearCache()
	cfg, err = confii.New[any](context.Background())
	require.NoError(t, err)
	port, err := cfg.Get("server.port")
	require.NoError(t, err)
	assert.Equal(t, 9090, port)
}

func TestInitCommandScaffoldsSectionedFileThatLoads(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "billing-service")
	_, err := execCobra(NewInitCmd(), []string{
		"--strategy", "sectioned",
		"--default-environment", "development",
		"--environments", "development,production",
		dir,
	})
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, "config", "application.yaml"))
	assert.NoFileExists(t, filepath.Join(dir, "config", "default.yaml"))

	selfConfig := readTestFile(t, filepath.Join(dir, selfConfigFilename))
	assert.Contains(t, selfConfig, "environment_strategy: sectioned")
	assert.Contains(t, selfConfig, `path: "config/application.yaml"`)

	withWorkingDirectory(t, dir)
	selfconfig.ClearCache()
	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)
	assert.Equal(t, "billing-service", getTestString(t, cfg, "app.name"))
	assert.Equal(t, "debug", getTestString(t, cfg, "log.level"))
}

func TestInitCommandIsIdempotentForExistingProject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "confii.yaml")
	require.NoError(t, os.WriteFile(path, []byte("deep_merge: true\n"), 0o600))

	selfconfig.ClearCache()
	out, err := execCobra(NewInitCmd(), []string{dir})
	require.NoError(t, err)
	assert.Contains(t, out, "Already initialized")
	assert.Contains(t, out, "No files changed")
	assert.NoFileExists(t, filepath.Join(dir, selfConfigFilename))
	assert.NoDirExists(t, filepath.Join(dir, "config"))
	assert.Equal(t, "deep_merge: true\n", readTestFile(t, path))
}

func TestInitCommandRejectsInvalidExistingProject(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, selfConfigFilename), []byte("sources: [\n"), 0o600))

	selfconfig.ClearCache()
	_, err := execCobra(NewInitCmd(), []string{dir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "appears initialized")
	assert.Contains(t, err.Error(), "invalid")
}

func TestInitCommandRejectsMisspelledExistingSetting(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, selfConfigFilename), []byte("env_swticher: APP_ENV\n"), 0o600))

	selfconfig.ClearCache()
	_, err := execCobra(NewInitCmd(), []string{dir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "env_swticher")
}

func TestInitCommandRejectsAmbiguousSelfConfiguration(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"), []byte("deep_merge: true\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, selfConfigFilename), []byte("deep_merge: false\n"), 0o600))

	_, err := execCobra(NewInitCmd(), []string{dir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple Confii")
}

func TestInitCommandForceDoesNotCompeteWithAlternateSelfConfig(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.toml"), []byte("deep_merge = true\n"), 0o600))

	_, err := execCobra(NewInitCmd(), []string{"--force", dir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "competing")
	assert.NoFileExists(t, filepath.Join(dir, selfConfigFilename))
}

func TestInitCommandPreflightsEveryOutputBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o750))
	existing := filepath.Join(configDir, "production.yaml")
	require.NoError(t, os.WriteFile(existing, []byte("owned: by-user\n"), 0o600))

	_, err := execCobra(NewInitCmd(), []string{"--strategy", "named-files", dir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no files changed")
	assert.NoFileExists(t, filepath.Join(dir, selfConfigFilename))
	assert.NoFileExists(t, filepath.Join(configDir, "default.yaml"))
	assert.Equal(t, "owned: by-user\n", readTestFile(t, existing))
}

func TestInitCommandDryRunHasNoFilesystemEffects(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-created")
	out, err := execCobra(NewInitCmd(), []string{"--dry-run", "--strategy", "sectioned", dir})
	require.NoError(t, err)
	assert.Contains(t, out, "Would create")
	assert.Contains(t, out, "application.yaml")
	assert.NoDirExists(t, dir)
}

func TestInitCommandCustomizesProjectLayout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "custom")
	_, err := execCobra(NewInitCmd(), []string{
		"--strategy", "named_files",
		"--config-dir", "deploy/settings",
		"--default-environment", "local",
		"--environments", "prod,local,prod",
		"--env-switcher", "SERVICE_STAGE",
		dir,
	})
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, "deploy", "settings", "default.yaml"))
	assert.FileExists(t, filepath.Join(dir, "deploy", "settings", "local.yaml"))
	assert.FileExists(t, filepath.Join(dir, "deploy", "settings", "prod.yaml"))
	assert.NoFileExists(t, filepath.Join(dir, "deploy", "settings", "development.yaml"))

	data := readTestFile(t, filepath.Join(dir, selfConfigFilename))
	assert.Contains(t, data, `env_switcher: "SERVICE_STAGE"`)
	assert.Contains(t, data, `search_paths: ["deploy/settings"]`)
}

func TestInitLayoutPrompt(t *testing.T) {
	tests := []struct {
		input string
		want  initLayout
	}{
		{input: "\n", want: initLayoutNamedFiles},
		{input: "1\n", want: initLayoutNamedFiles},
		{input: "2\n", want: initLayoutSectioned},
		{input: "3\n", want: initLayoutMinimal},
	}
	for _, test := range tests {
		var output bytes.Buffer
		got, err := promptInitLayout(strings.NewReader(test.input), &output)
		require.NoError(t, err)
		assert.Equal(t, test.want, got)
		assert.Contains(t, output.String(), "Separate files (recommended)")
	}

	_, err := promptInitLayout(strings.NewReader("9\n"), &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid selection")

	_, err = promptInitLayout(errorReader{}, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read initialization choice")

	_, err = promptInitLayout(strings.NewReader("1\n"), errorWriter{})
	require.Error(t, err)
}

func TestResolveInitLayoutModes(t *testing.T) {
	cmd := NewInitCmd()
	cmd.SetIn(strings.NewReader("2\n"))
	var output bytes.Buffer
	cmd.SetOut(&output)

	layout, err := resolveInitLayout(cmd, &initOptions{})
	require.NoError(t, err)
	assert.Equal(t, initLayoutSectioned, layout)

	layout, err = resolveInitLayout(cmd, &initOptions{nonInteractive: true})
	require.NoError(t, err)
	assert.Equal(t, initLayoutNamedFiles, layout)

	layout, err = resolveInitLayout(cmd, &initOptions{minimal: true})
	require.NoError(t, err)
	assert.Equal(t, initLayoutMinimal, layout)
}

func TestInitOptionValidation(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "strategy", args: []string{"--strategy", "hybrid"}, want: "invalid --strategy"},
		{name: "minimal strategy", args: []string{"--minimal", "--strategy", "sectioned"}, want: "cannot be combined"},
		{name: "absolute config dir", args: []string{"--config-dir", filepath.Join(string(filepath.Separator), "tmp")}, want: "relative"},
		{name: "escaping config dir", args: []string{"--config-dir", "../outside"}, want: "within"},
		{name: "empty switcher", args: []string{"--env-switcher", ""}, want: "must not be empty"},
		{name: "bad switcher", args: []string{"--env-switcher", "APP-ENV"}, want: "OS variable name"},
		{name: "bad default env", args: []string{"--default-environment", "../prod"}, want: "invalid --default-environment"},
		{name: "reserved env", args: []string{"--environments", "default"}, want: "reserved"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "project")
			args := append(test.args, dir)
			_, err := execCobra(NewInitCmd(), args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.want)
			assert.NoDirExists(t, dir)
		})
	}
}

func TestInitCommandReturnsPlanWriterError(t *testing.T) {
	dir := t.TempDir()
	cmd := NewInitCmd()
	cmd.SetOut(errorWriter{})
	cmd.SetArgs([]string{"--dry-run", dir})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	require.Error(t, cmd.Execute())
}

func TestInitRenderTemplateAndHelpers(t *testing.T) {
	for _, alias := range []string{"separate", "separate-files", "single", "single-file"} {
		_, err := parseInitLayout(alias)
		require.NoError(t, err)
	}

	assert.NoError(t, validateEnvSwitcher("_APP_ENV2"))
	assert.Error(t, validateEnvSwitcher("2APP"))
	assert.Error(t, validateInitEnvironment("-prod"))
	assert.Error(t, validateInitEnvironment(""))

	values, err := normalizeInitEnvironments([]string{"production", "development"}, "development")
	require.NoError(t, err)
	assert.Equal(t, []string{"development", "production"}, values)

	assert.Equal(t, ".", mustCleanProjectDir(t, ""))
	assert.Equal(t, ".", mustCleanProjectDir(t, "."))
	assert.NotEmpty(t, initProjectName("."))

	unknownSection := string(renderSectionedStarter("service", []string{"staging"}))
	assert.Contains(t, unknownSection, "staging:\n  {}")

	replaced, err := replaceSingleInitSetting("a: 1\n", "a: 1", "a: 2")
	require.NoError(t, err)
	assert.Equal(t, "a: 2\n", replaced)
	_, err = replaceSingleInitSetting("a: 1\na: 1\n", "a: 1", "a: 2")
	require.Error(t, err)

	_, err = renderInitSelfConfigTemplate("not the canonical template", initLayoutSectioned, "config", "development", "APP_ENV")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "template drift")

	_, err = buildInitPlanWithRenderer(
		".",
		initLayoutSectioned,
		initOptions{defaultEnvironment: "development", envSwitcher: "APP_ENV", configDir: "config"},
		func(initLayout, string, string, string) ([]byte, error) {
			return nil, errors.New("render failed")
		},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "render failed")
}

func TestInitPreflightRejectsNonRegularTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.Mkdir(target, 0o750))
	err := preflightInitPlan(dir, []initFile{{path: target, data: []byte("x")}}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a regular file")
}

func TestInitPreflightReportsInspectionErrors(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "parent")
	require.NoError(t, os.WriteFile(parent, []byte("file"), 0o600))
	err := preflightInitPlan(dir, []initFile{{path: filepath.Join(parent, "child")}}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-directory parent")
}

func TestInitPreflightRejectsOutsideAndSymlinkedTargets(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	err := ensureInitTargetInsideProject(root, outside)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside project root")

	if runtime.GOOS == "windows" {
		return
	}
	externalDir := t.TempDir()
	link := filepath.Join(root, "config")
	require.NoError(t, os.Symlink(externalDir, link))
	err = preflightInitPlan(root, []initFile{{path: filepath.Join(link, "default.yaml")}}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symbolic link")
}

func TestInitPathInspectionErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not enforce Unix directory traversal bits")
	}
	root := t.TempDir()
	protected := filepath.Join(root, "protected")
	require.NoError(t, os.Mkdir(protected, 0o700))
	require.NoError(t, os.Chmod(protected, 0o000))
	t.Cleanup(func() { _ = os.Chmod(protected, 0o700) })

	err := preflightInitPlan(root, []initFile{{path: filepath.Join(protected, "file.yaml")}}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inspect initialization target")

	err = ensureInitTargetInsideProject(root, filepath.Join(protected, "nested", "file.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inspect initialization parent")
}

func TestInitForceReplacesCompletePlan(t *testing.T) {
	dir := t.TempDir()
	selfPath := filepath.Join(dir, selfConfigFilename)
	configPath := filepath.Join(dir, "config", "application.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o750))
	require.NoError(t, os.WriteFile(selfPath, []byte("old self\n"), 0o600))
	require.NoError(t, os.WriteFile(configPath, []byte("old config\n"), 0o640))

	_, err := execCobra(NewInitCmd(), []string{"--force", "--strategy", "sectioned", dir})
	require.NoError(t, err)
	assert.NotContains(t, readTestFile(t, selfPath), "old self")
	assert.NotContains(t, readTestFile(t, configPath), "old config")
}

func TestWriteInitPlanRollsBackNewFiles(t *testing.T) {
	dir := t.TempDir()
	blockedParent := filepath.Join(dir, "blocked")
	require.NoError(t, os.WriteFile(blockedParent, []byte("file"), 0o600))
	first := filepath.Join(dir, "first.yaml")
	plan := []initFile{
		{path: first, data: []byte("created: true\n")},
		{path: filepath.Join(blockedParent, "second.yaml"), data: []byte("never: true\n")},
	}
	err := writeInitPlan(plan, false)
	require.Error(t, err)
	assert.NoFileExists(t, first)
}

func TestWriteInitPlanRestoresForcedFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not enforce Unix directory write bits")
	}
	dir := t.TempDir()
	blockedParent := filepath.Join(dir, "blocked")
	require.NoError(t, os.Mkdir(blockedParent, 0o500))
	t.Cleanup(func() { _ = os.Chmod(blockedParent, 0o700) })
	first := filepath.Join(dir, "first.yaml")
	require.NoError(t, os.WriteFile(first, []byte("original: true\n"), 0o640))
	plan := []initFile{
		{path: first, data: []byte("replacement: true\n")},
		{path: filepath.Join(blockedParent, "second.yaml"), data: []byte("never: true\n")},
	}
	err := writeInitPlan(plan, true)
	require.Error(t, err)
	assert.Equal(t, "original: true\n", readTestFile(t, first))
}

func TestInitRollbackReportsIncompleteCleanup(t *testing.T) {
	dir := t.TempDir()
	nonEmpty := filepath.Join(dir, "non-empty")
	require.NoError(t, os.Mkdir(nonEmpty, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(nonEmpty, "child"), []byte("x"), 0o600))
	err := withInitRollback(errors.New("original failure"), []string{nonEmpty}, nil, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rollback was incomplete")
}

func TestInitRollbackReportsRestoreFailure(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "parent")
	require.NoError(t, os.WriteFile(parent, []byte("file"), 0o600))
	path := filepath.Join(parent, "child")
	err := withInitRollback(
		errors.New("original failure"),
		[]string{path},
		map[string]initFileBackup{path: {data: []byte("old"), mode: 0o600, exists: true}},
		true,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restore")
}

func TestWriteInitPlanReportsBackupFailures(t *testing.T) {
	dir := t.TempDir()
	directoryTarget := filepath.Join(dir, "directory")
	require.NoError(t, os.Mkdir(directoryTarget, 0o750))
	err := writeInitPlan([]initFile{{path: directoryTarget, data: []byte("x")}}, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "back up")

	parent := filepath.Join(dir, "parent-file")
	require.NoError(t, os.WriteFile(parent, []byte("x"), 0o600))
	err = writeInitPlan([]initFile{{path: filepath.Join(parent, "child"), data: []byte("x")}}, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "back up")
}

func TestInitOutputHelpersReturnWriterErrors(t *testing.T) {
	err := printInitPlan(errorWriter{}, "Created", initLayoutMinimal, []initFile{{path: "x"}})
	require.Error(t, err)
	err = printInitNextSteps(errorWriter{}, initLayoutNamedFiles, ".", "development", "APP_ENV", false)
	require.Error(t, err)
}

func TestInitNextStepsMatchLayoutAndTarget(t *testing.T) {
	var named bytes.Buffer
	require.NoError(t, printInitNextSteps(&named, initLayoutNamedFiles, "path with spaces", "development", "APP_ENV", true))
	assert.Contains(t, named.String(), `cd "path with spaces"`)
	assert.Contains(t, named.String(), "APP_ENV=development confii plan")
	assert.Contains(t, named.String(), "never removes files")

	var minimal bytes.Buffer
	require.NoError(t, printInitNextSteps(&minimal, initLayoutMinimal, ".", "development", "APP_ENV", false))
	assert.Contains(t, minimal.String(), "Edit .confii.yaml")
	assert.NotContains(t, minimal.String(), "APP_ENV=development")
}

func TestInitNextStepsReportsEveryWriterFailure(t *testing.T) {
	for writes := 0; writes <= 6; writes++ {
		err := printInitNextSteps(
			&failAfterWriter{writesBeforeFailure: writes},
			initLayoutNamedFiles,
			"another project",
			"development",
			"APP_ENV",
			true,
		)
		require.Error(t, err, "named layout write %d", writes)
	}
	for writes := 0; writes <= 4; writes++ {
		err := printInitNextSteps(
			&failAfterWriter{writesBeforeFailure: writes},
			initLayoutMinimal,
			".",
			"development",
			"APP_ENV",
			false,
		)
		require.Error(t, err, "minimal layout write %d", writes)
	}
}

func TestPrintInitPlanReportsLaterWriterError(t *testing.T) {
	writer := &failAfterWriter{writesBeforeFailure: 1}
	err := printInitPlan(writer, "Created", initLayoutMinimal, []initFile{{path: "x"}})
	require.Error(t, err)
}

func TestWriteSelfConfigRejectsExistingAndInvalidTargets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))
	err := writeSelfConfig(path, []byte("new"), false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")

	err = writeSelfConfig(filepath.Join(path, "child"), []byte("new"), false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create")
}

func mustCleanProjectDir(t *testing.T, value string) string {
	t.Helper()
	result, err := cleanProjectRelativeDir(value)
	require.NoError(t, err)
	return result
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func getTestString(t *testing.T, cfg *confii.Config[any], path string) string {
	t.Helper()
	value, err := cfg.GetString(path)
	require.NoError(t, err)
	return value
}

func withWorkingDirectory(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() {
		_ = os.Chdir(previous)
		selfconfig.ClearCache()
	})
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

type failAfterWriter struct {
	writesBeforeFailure int
}

func (w *failAfterWriter) Write(data []byte) (int, error) {
	if w.writesBeforeFailure == 0 {
		return 0, errors.New("write failed")
	}
	w.writesBeforeFailure--
	return len(data), nil
}
