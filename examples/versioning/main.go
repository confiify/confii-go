// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package main demonstrates config versioning: taking snapshots,
// comparing versions, and rolling back to a previous state.
package main

import (
	"fmt"
	"log"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
)

func main() {
	cfg, err := confii.New[any](confii.WithLoaders(loader.NewYAML("../lifecycle/config.yaml")))
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
	if err := cfg.Set("app.debug", false); err != nil {
		log.Fatal(err)
	}
	if err := cfg.Set("database.host", "new-db.example.com"); err != nil {
		log.Fatal(err)
	}

	// Take another snapshot
	v2, err := cfg.SaveVersion(nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Saved version:", v2.VersionID)

	// Compare versions
	diffs, err := vm.DiffVersions(v1.VersionID, v2.VersionID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nChanges between v1 and v2: %d\n", len(diffs))
	for _, d := range diffs {
		fmt.Printf("  %s: %s (%v -> %v)\n", d.Path, d.Type, d.OldValue, d.NewValue)
	}

	// List all versions
	versions := vm.ListVersions()
	fmt.Printf("\nStored versions: %d\n", len(versions))

	// Rollback to v1
	err = cfg.RollbackToVersion(v1.VersionID)
	if err != nil {
		log.Fatal(err)
	}
	host, err := cfg.Get("database.host")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("\nAfter rollback, host:", host) // localhost
}
