// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// security-insights-check validates Confii's Security Insights document with
// the official OpenSSF v2 tooling and checks repository-specific invariants.
package main

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
	"github.com/ossf/si-tooling/v2/si"
)

const expectedRepository = "https://github.com/confiify/confii-go"

func main() {
	path := "security-insights.yml"
	minderPath := ""
	if len(os.Args) >= 2 {
		path = os.Args[1]
	}
	if len(os.Args) == 3 {
		minderPath = os.Args[2]
	} else if len(os.Args) > 3 {
		fmt.Fprintf(os.Stderr, "usage: %s [security-insights.yml] [minder-profile.yml]\n", os.Args[0])
		os.Exit(2)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		fail("read %s: %v", path, err)
	}
	insights, err := si.Load(contents)
	if err != nil {
		fail("validate %s: %v", path, err)
	}
	if insights.Project == nil || insights.Project.Name != "Confii" {
		fail("%s must describe the Confii project", path)
	}
	if insights.Repository == nil || insights.Repository.Url.String() != expectedRepository {
		fail("%s must describe %s", path, expectedRepository)
	}
	if !insights.Project.VulnerabilityReporting.ReportsAccepted || insights.Project.VulnerabilityReporting.Policy == nil {
		fail("%s must publish an enabled vulnerability-reporting policy", path)
	}

	fmt.Printf("%s conforms to Security Insights schema %s\n", path, insights.Header.SchemaVersion.String())
	if minderPath != "" {
		validateMinderProfile(minderPath)
	}
}

type minderProfile struct {
	Version   string `yaml:"version"`
	Type      string `yaml:"type"`
	Name      string `yaml:"name"`
	Alert     string `yaml:"alert"`
	Remediate string `yaml:"remediate"`
	Context   struct {
		Provider string `yaml:"provider"`
	} `yaml:"context"`
	Repository []struct {
		Type string `yaml:"type"`
	} `yaml:"repository"`
}

func validateMinderProfile(path string) {
	contents, err := os.ReadFile(path)
	if err != nil {
		fail("read %s: %v", path, err)
	}
	var profile minderProfile
	if err := yaml.Unmarshal(contents, &profile); err != nil {
		fail("parse %s: %v", path, err)
	}
	if profile.Version != "v1" || profile.Type != "profile" || profile.Name == "" || profile.Context.Provider != "github" {
		fail("%s is not a named Minder v1 GitHub profile", path)
	}
	if profile.Alert != "off" || profile.Remediate != "off" {
		fail("%s must remain audit-only until a separate security review", path)
	}
	if len(profile.Repository) < 10 {
		fail("%s contains only %d repository rules; expected the Confii security baseline", path, len(profile.Repository))
	}
	seen := make(map[string]bool, len(profile.Repository))
	for _, rule := range profile.Repository {
		seen[rule.Type] = true
	}
	for _, required := range []string{"secret_scanning", "branch_protection_require_signatures", "actions_check_pinned_tags", "security_insights", "codeql_enabled"} {
		if !seen[required] {
			fail("%s is missing required Minder rule %s", path, required)
		}
	}
	fmt.Printf("%s is a valid audit-only Minder profile with %d repository rules\n", path, len(profile.Repository))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
