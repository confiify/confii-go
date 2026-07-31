// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	rootModule   = "github.com/confiify/confii-go/v2"
	loaderModule = "github.com/confiify/confii-go/loader/cloud/v2"
	secretModule = "github.com/confiify/confii-go/secret/cloud/v2"
	zeroVersion  = "v2.0.0-00010101000000-000000000000"
)

func TestDevConsumerUnlinkDropsSyntheticZeroRequirements(t *testing.T) {
	consumer, repo := writeConsumerFixture(t, "v2.0.0", zeroVersion, zeroVersion)
	runDevConsumer(t, "unlink", consumer, repo, "all", "")

	mod := readGoMod(t, consumer)
	assertNotContains(t, mod, "replace "+rootModule)
	assertNotContains(t, mod, loaderModule+" "+zeroVersion)
	assertNotContains(t, mod, secretModule+" "+zeroVersion)
	assertContains(t, mod, rootModule+" v2.0.0")
}

func TestDevConsumerUnlinkPinsRequestedRelease(t *testing.T) {
	consumer, repo := writeConsumerFixture(t, zeroVersion, zeroVersion, zeroVersion)
	runDevConsumer(t, "unlink", consumer, repo, "all", "v2.0.0")

	mod := readGoMod(t, consumer)
	for _, module := range []string{rootModule, loaderModule, secretModule} {
		assertContains(t, mod, module+" v2.0.0")
		assertNotContains(t, mod, "replace "+module)
	}
}

func TestDevConsumerUnlinkCorePinDropsUnselectedZeroRequirements(t *testing.T) {
	consumer, repo := writeConsumerFixture(t, zeroVersion, zeroVersion, zeroVersion)
	runDevConsumer(t, "unlink", consumer, repo, "core", "v2.0.0")

	mod := readGoMod(t, consumer)
	assertContains(t, mod, rootModule+" v2.0.0")
	assertNotContains(t, mod, loaderModule+" "+zeroVersion)
	assertNotContains(t, mod, secretModule+" "+zeroVersion)
}

func TestDevConsumerUnlinkRejectsZeroRelease(t *testing.T) {
	consumer, repo := writeConsumerFixture(t, "v2.0.0", "v2.0.0", "v2.0.0")
	cmd := exec.Command("sh", "dev-consumer.sh", "unlink", consumer, repo, "all", zeroVersion)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("unlink unexpectedly accepted the zero pseudo-version")
	}
	assertContains(t, string(output), "must identify a real release or revision")
}

func writeConsumerFixture(t *testing.T, rootVersion, loaderVersion, secretVersion string) (string, string) {
	t.Helper()
	base := t.TempDir()
	consumer := filepath.Join(base, "consumer")
	repo := filepath.Join(base, "confii-go")
	for _, dir := range []string{consumer, repo, filepath.Join(repo, "loader", "cloud"), filepath.Join(repo, "secret", "cloud")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module "+rootModule+"\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mod := "module example.com/consumer\n\ngo 1.25.0\n\nrequire (\n" +
		"\t" + rootModule + " " + rootVersion + "\n" +
		"\t" + loaderModule + " " + loaderVersion + "\n" +
		"\t" + secretModule + " " + secretVersion + "\n" +
		")\n\n" +
		"replace " + rootModule + " => " + repo + "\n\n" +
		"replace " + loaderModule + " => " + filepath.Join(repo, "loader", "cloud") + "\n\n" +
		"replace " + secretModule + " => " + filepath.Join(repo, "secret", "cloud") + "\n"
	if err := os.WriteFile(filepath.Join(consumer, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	return consumer, repo
}

func runDevConsumer(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("sh", append([]string{"dev-consumer.sh"}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dev-consumer.sh failed: %v\n%s", err, output)
	}
}

func readGoMod(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertContains(t *testing.T, value, fragment string) {
	t.Helper()
	if !strings.Contains(value, fragment) {
		t.Fatalf("expected %q to contain %q", value, fragment)
	}
}

func assertNotContains(t *testing.T, value, fragment string) {
	t.Helper()
	if strings.Contains(value, fragment) {
		t.Fatalf("expected %q not to contain %q", value, fragment)
	}
}
