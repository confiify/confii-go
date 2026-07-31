// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package main demonstrates exporting configuration to different formats
// (JSON, YAML, TOML) and generating documentation.
package main

import (
	"fmt"
	"log"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
)

func main() {
	cfg, err := confii.New[any](confii.WithLoaders(loader.NewYAML("config.yaml")))
	if err != nil {
		log.Fatal(err)
	}

	// Export to JSON (returns bytes)
	jsonData, err := cfg.Export("json")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("=== JSON ===")
	fmt.Println(string(jsonData))

	// Export to YAML
	yamlData, err := cfg.Export("yaml")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("=== YAML ===")
	fmt.Println(string(yamlData))

	// Export to TOML
	tomlData, err := cfg.Export("toml")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("=== TOML ===")
	fmt.Println(string(tomlData))

	// Export to file
	if _, err := cfg.Export("json", "/tmp/config-export.json"); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Exported to /tmp/config-export.json")

	// Generate documentation
	markdown, err := cfg.GenerateDocs("markdown")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("\n=== Generated Docs ===")
	fmt.Println(string(markdown))
}
