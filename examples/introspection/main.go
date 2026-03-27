// Package main demonstrates Confii's introspection capabilities:
// Explain, Layers, Schema, source tracking, and debug reports.
package main

import (
	"context"
	"fmt"
	"log"

	confii "github.com/qualitycoe/confii-go"
	"github.com/qualitycoe/confii-go/loader"
)

func main() {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(
			loader.NewYAML("base.yaml"),
			loader.NewYAML("overrides.yaml"),
		),
		confii.WithDeepMerge(true),
		confii.WithDebugMode(true), // enables full source tracking
	)
	if err != nil {
		log.Fatal(err)
	}

	// Explain: detailed resolution info for a key
	info := cfg.Explain("database.host")
	fmt.Println("=== Explain ===")
	fmt.Printf("%+v\n\n", info)

	// Schema: type info for a key
	schema := cfg.Schema("database.port")
	fmt.Println("=== Schema ===")
	fmt.Printf("%+v\n\n", schema)

	// Layers: show the source stack
	layers := cfg.Layers()
	fmt.Println("=== Layers ===")
	for _, layer := range layers {
		fmt.Printf("  %v\n", layer)
	}
	fmt.Println()

	// Source tracking
	srcInfo := cfg.GetSourceInfo("database.host")
	fmt.Println("=== Source Info ===")
	fmt.Printf("  Value: %v, Source: %s, Overrides: %d\n",
		srcInfo.Value, srcInfo.SourceFile, srcInfo.OverrideCount)

	// Override history
	history := cfg.GetOverrideHistory("database.host")
	fmt.Println("\n=== Override History ===")
	for _, entry := range history {
		fmt.Printf("  %v from %s\n", entry.Value, entry.Source)
	}

	// Conflicts: all keys that were overridden
	conflicts := cfg.GetConflicts()
	fmt.Printf("\n=== Conflicts (%d keys) ===\n", len(conflicts))
	for key := range conflicts {
		fmt.Printf("  %s\n", key)
	}

	// Debug report
	fmt.Println("\n=== Debug Report ===")
	fmt.Print(cfg.PrintDebugInfo("database.host"))
}
