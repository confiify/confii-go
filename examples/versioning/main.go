// Package main demonstrates config versioning: taking snapshots,
// comparing versions, and rolling back to a previous state.
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
		confii.WithLoaders(loader.NewYAML("../lifecycle/config.yaml")),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Enable versioning (storage path, max versions to keep)
	vm := cfg.EnableVersioning("/tmp/confii-versions", 100)

	// Take initial snapshot
	v1, err := cfg.SaveVersion(map[string]any{
		"author": "deploy-bot",
		"env":    "production",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Saved version:", v1.VersionID)

	// Make changes
	cfg.Set("app.debug", false)
	cfg.Set("database.host", "new-db.example.com")

	// Take another snapshot
	v2, _ := cfg.SaveVersion(nil)
	fmt.Println("Saved version:", v2.VersionID)

	// Compare versions
	diffs, _ := vm.DiffVersions(v1.VersionID, v2.VersionID)
	fmt.Printf("\nChanges between v1 and v2: %d\n", len(diffs))
	for _, d := range diffs {
		fmt.Printf("  %s: %s (%v -> %v)\n", d["key"], d["type"], d["old_value"], d["new_value"])
	}

	// List all versions
	versions := vm.ListVersions()
	fmt.Printf("\nStored versions: %d\n", len(versions))

	// Rollback to v1
	err = cfg.RollbackToVersion(v1.VersionID)
	if err != nil {
		log.Fatal(err)
	}
	host, _ := cfg.Get("database.host")
	fmt.Println("\nAfter rollback, host:", host) // localhost
}
