// Package main demonstrates config diffing and drift detection.
// Compare two Config instances or detect drift from an intended baseline.
package main

import (
	"context"
	"fmt"
	"log"

	confii "github.com/qualitycoe/confii-go"
	"github.com/qualitycoe/confii-go/diff"
	"github.com/qualitycoe/confii-go/loader"
)

func main() {
	ctx := context.Background()

	cfg1, err := confii.New[any](ctx,
		confii.WithLoaders(loader.NewYAML("../introspection/base.yaml")),
	)
	if err != nil {
		log.Fatal(err)
	}

	cfg2, err := confii.New[any](ctx,
		confii.WithLoaders(loader.NewYAML("../introspection/overrides.yaml")),
	)
	if err != nil {
		log.Fatal(err)
	}

	// --- Diff two configs ---
	diffs := cfg1.Diff(cfg2)
	fmt.Println("=== Config Diff ===")
	for _, d := range diffs {
		fmt.Printf("  [%s] %s: %v -> %v\n", d.Type, d.Key, d.OldValue, d.NewValue)
	}

	// Summary
	summary := diff.Summary(diffs)
	fmt.Printf("\nSummary: %v\n", summary)

	// --- Drift detection ---
	intended := map[string]any{
		"database.host": "prod-db.example.com",
		"database.port": 5432,
		"app.debug":     false,
	}

	drifts := cfg1.DetectDrift(intended)
	fmt.Println("\n=== Drift Detection ===")
	for _, d := range drifts {
		fmt.Printf("  [%s] %s: expected %v, got %v\n",
			d.Type, d.Key, d.NewValue, d.OldValue)
	}
}
