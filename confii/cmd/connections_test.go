// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnectionsTestResolvesSecretsWithoutPrintingValues(t *testing.T) {
	dir := t.TempDir()
	writeConnectionFixture(t, dir, `.confii.yaml`, `
default_environment: production
sources:
  - type: yaml
    path: app.yaml
secrets:
  provider: dict
  entries:
    service-token: highly-sensitive-value
`)
	writeConnectionFixture(t, dir, `app.yaml`, `
default:
  endpoint: https://example.invalid
  token: ${secret:service-token}
production:
  workers: 3
`)
	withWorkingDirectory(t, dir)
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	cmd := NewConnectionsCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"test", "production"})
	require.NoError(t, cmd.Execute())

	text := output.String()
	assert.Contains(t, text, "Connection test: OK")
	assert.Contains(t, text, "Secret provider: dict (1 references resolved)")
	assert.Contains(t, text, "Values checked: 3 (contents withheld)")
	assert.NotContains(t, text, "highly-sensitive-value")
	assert.NotContains(t, text, "service-token")
}

func TestConnectionsTestJSONAndSelectedKey(t *testing.T) {
	dir := t.TempDir()
	writeConnectionFixture(t, dir, `.confii.yaml`, "default_files: [app.yaml]\n")
	writeConnectionFixture(t, dir, `app.yaml`, "alpha: one\nbeta: two\n")
	withWorkingDirectory(t, dir)
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	cmd := NewConnectionsCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"test", "--key", "beta", "--json"})
	require.NoError(t, cmd.Execute())

	var report connectionReport
	require.NoError(t, json.Unmarshal(output.Bytes(), &report))
	assert.Equal(t, "ok", report.Status)
	assert.Equal(t, 1, report.KeysChecked)
	assert.Len(t, report.Sources, 1)
	assert.Empty(t, report.SecretProvider)
	assert.NotContains(t, output.String(), "two")
}

func TestConnectionsTestRejectsUnverifiedSecretProvider(t *testing.T) {
	dir := t.TempDir()
	writeConnectionFixture(t, dir, `.confii.yaml`, `
default_files: [app.yaml]
secrets:
  provider: dict
  entries:
    unused: sensitive
`)
	writeConnectionFixture(t, dir, `app.yaml`, "plain: value\n")
	withWorkingDirectory(t, dir)
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	cmd := NewConnectionsCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"test"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not verify secret provider")
	assert.NotContains(t, err.Error(), "sensitive")
}

func TestConnectionsTestSelectedParentVerifiesNestedSecret(t *testing.T) {
	dir := t.TempDir()
	writeConnectionFixture(t, dir, `.confii.yaml`, `
default_files: [app.yaml]
secrets:
  provider: dict
  entries:
    password: hidden
`)
	writeConnectionFixture(t, dir, `app.yaml`, "database:\n  password: ${secret:password}\n  host: localhost\n")
	withWorkingDirectory(t, dir)
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	cmd := NewConnectionsCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"test", "--key", "database"})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, output.String(), "1 references resolved")
	assert.NotContains(t, output.String(), "hidden")
}

func TestConnectionsTestHonorsTimeoutWithoutLeakingSource(t *testing.T) {
	const provider = "test-slow-connection"
	confii.RegisterSelfConfigSourceProvider(provider, func(context.Context, map[string]any) (confii.Loader, error) {
		return slowConnectionLoader{}, nil
	})
	dir := t.TempDir()
	writeConnectionFixture(t, dir, `.confii.yaml`, "sources:\n  - type: "+provider+"\n")
	withWorkingDirectory(t, dir)
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	cmd := NewConnectionsCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"test", "--timeout", "10ms"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection test failed")
	assert.Contains(t, err.Error(), "deadline was exceeded")
	assert.NotContains(t, err.Error(), "must-not-leak")
}

type slowConnectionLoader struct{}

func (slowConnectionLoader) Source() string {
	return "https://example.invalid/config?token=must-not-leak"
}

func (slowConnectionLoader) Load(ctx context.Context) (map[string]any, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestConnectionCommandErrorBranches(t *testing.T) {
	dir := t.TempDir()
	writeConnectionFixture(t, dir, `.confii.yaml`, "default_files: [app.yaml]\n")
	writeConnectionFixture(t, dir, `app.yaml`, "present: value\n")
	withWorkingDirectory(t, dir)
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	for _, args := range [][]string{
		{"test", "--key", "   "},
		{"test", "--key", "missing"},
		{"test", "--loader", "unknown:source"},
	} {
		cmd := NewConnectionsCmd()
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetErr(new(bytes.Buffer))
		cmd.SetArgs(args)
		assert.Error(t, cmd.Execute(), args)
	}

	assert.NoError(t, connectionFailure("unused", nil))
	unknown := errors.New("token=must-not-leak")
	err := connectionFailure("authenticate", unknown)
	assert.Contains(t, err.Error(), "details withheld")
	assert.NotContains(t, err.Error(), "must-not-leak")
	assert.ErrorIs(t, err, unknown)
}

func TestConnectionFailureClassificationsDoNotLeakCauses(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "deadline"},
		{context.Canceled, "canceled"},
		{confii.ErrVaultAuth, "authentication"},
		{confii.ErrSecretNotFound, "not found"},
		{confii.ErrSecretAccess, "secret provider"},
		{confii.ErrSecretStore, "secret provider"},
		{confii.ErrSecretValidation, "invalid value"},
		{confii.ErrConfigNotFound, "configuration key"},
		{confii.ErrConfigLoad, "configured source"},
	}
	for _, test := range tests {
		got := connectionFailure("test", fmt.Errorf("sensitive material: %w", test.err))
		assert.Contains(t, got.Error(), test.want)
		assert.NotContains(t, got.Error(), "sensitive material")
		assert.ErrorIs(t, got, test.err)
	}
}

func TestPrintConnectionReportPropagatesWriterFailure(t *testing.T) {
	cmd := NewConnectionsCmd()
	cmd.SetOut(failingConnectionWriter{})
	err := printConnectionReport(cmd, connectionReport{})
	require.Error(t, err)
}

type failingConnectionWriter struct{}

func (failingConnectionWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestNewRootCommand(t *testing.T) {
	var output bytes.Buffer
	root := NewRootCommand("v-test", &output, &output)
	root.SetArgs([]string{"--version"})
	require.NoError(t, root.Execute())
	assert.Contains(t, output.String(), "v-test")
}

func writeConnectionFixture(t *testing.T, dir, name, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600))
}
