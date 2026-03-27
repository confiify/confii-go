// Package main demonstrates observability features: access metrics,
// event emission, and monitoring config usage patterns.
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
		confii.WithLoaders(loader.NewYAML("../basic/config.yaml")),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Enable metrics collection
	cfg.EnableObservability()

	// Enable event emission
	emitter := cfg.EnableEvents()
	emitter.On("reload", func(args ...any) {
		fmt.Println("Event: config reloaded")
	})
	emitter.On("change", func(args ...any) {
		fmt.Println("Event: config changed")
	})

	// Simulate some access patterns
	cfg.Get("database.host")
	cfg.Get("database.port")
	cfg.Get("database.host") // accessed twice
	cfg.Get("app.name")

	// Trigger a change event
	cfg.Set("app.debug", false)

	// View metrics
	stats := cfg.GetMetrics()
	fmt.Println("=== Metrics ===")
	fmt.Printf("  Total keys:    %v\n", stats["total_keys"])
	fmt.Printf("  Accessed keys: %v\n", stats["accessed_keys"])
	fmt.Printf("  Access rate:   %v\n", stats["access_rate"])
	fmt.Printf("  Change count:  %v\n", stats["change_count"])

	if topKeys, ok := stats["top_accessed_keys"]; ok {
		fmt.Printf("  Top keys:      %v\n", topKeys)
	}
}
