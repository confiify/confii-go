// Package main demonstrates config lifecycle operations:
// reload, extend, freeze, override, set, and change callbacks.
package main

import (
	"context"
	"fmt"
	"log"

	confii "github.com/qualitycoe/confii-go"
	"github.com/qualitycoe/confii-go/loader"
)

func main() {
	ctx := context.Background()

	cfg, err := confii.New[any](ctx,
		confii.WithLoaders(loader.NewYAML("config.yaml")),
	)
	if err != nil {
		log.Fatal(err)
	}

	// --- Change callbacks ---
	cfg.OnChange(func(key string, oldVal, newVal any) {
		fmt.Printf("Changed: %s = %v -> %v\n", key, oldVal, newVal)
	})

	// --- Set values ---
	cfg.Set("app.version", "2.0.0")

	// Protected set (errors if key exists)
	err = cfg.Set("app.name", "other", confii.WithOverride(false))
	fmt.Println("Protected set error:", err) // ErrConfigFrozen or override error

	// --- Temporary override ---
	restore, _ := cfg.Override(map[string]any{"database.host": "test-db"})
	host, _ := cfg.Get("database.host")
	fmt.Println("Overridden host:", host) // test-db
	restore()
	host, _ = cfg.Get("database.host")
	fmt.Println("Restored host:", host) // localhost

	// --- Reload ---
	err = cfg.Reload(ctx)
	fmt.Println("Reload error:", err)

	// Incremental reload (only changed files)
	err = cfg.Reload(ctx, confii.WithIncremental(true))
	fmt.Println("Incremental reload error:", err)

	// Dry run (validates without applying)
	err = cfg.Reload(ctx, confii.WithDryRun(true))
	fmt.Println("Dry run error:", err)

	// --- Freeze ---
	cfg.Freeze()
	err = cfg.Set("app.debug", false)
	fmt.Println("Set after freeze:", err) // ErrConfigFrozen
	fmt.Println("Is frozen:", cfg.IsFrozen())
}
