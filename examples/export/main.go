// Package main demonstrates exporting configuration to different formats
// (JSON, YAML, TOML) and generating documentation.
package main

import (
	"context"
	"fmt"
	"log"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
)

func main() {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("config.yaml")),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Export to JSON (returns bytes)
	jsonData, _ := cfg.Export("json")
	fmt.Println("=== JSON ===")
	fmt.Println(string(jsonData))

	// Export to YAML
	yamlData, _ := cfg.Export("yaml")
	fmt.Println("=== YAML ===")
	fmt.Println(string(yamlData))

	// Export to TOML
	tomlData, _ := cfg.Export("toml")
	fmt.Println("=== TOML ===")
	fmt.Println(string(tomlData))

	// Export to file
	_, _ = cfg.Export("json", "/tmp/config-export.json")
	fmt.Println("Exported to /tmp/config-export.json")

	// Generate documentation
	markdown, _ := cfg.GenerateDocs("markdown")
	fmt.Println("\n=== Generated Docs ===")
	fmt.Println(string(markdown))
}
