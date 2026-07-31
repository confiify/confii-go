// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package main demonstrates file watching and dynamic reloading.
// Config automatically reloads when the underlying files change on disk.
package main

import (
	"fmt"
	"log"
	"time"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
)

func main() {
	cfg, err := confii.New[any](confii.WithLoaders(loader.NewYAML("config.yaml")),
		confii.WithDynamicReloading(true), // enables fsnotify file watcher
		confii.WithReloadDebounce(150*time.Millisecond),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := cfg.Close(); err != nil {
			log.Printf("close configuration: %v", err)
		}
	}()

	// Register a callback to react to changes
	cfg.OnChange(func(key string, oldVal, newVal any) {
		fmt.Printf("Config changed: %s = %v -> %v\n", key, oldVal, newVal)
	})

	fmt.Println("Watching config.yaml for changes...")
	fmt.Println("Edit the file to see automatic reloading in action.")

	// Keep running to observe file changes
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		debug := cfg.GetBoolOr("app.debug", false)
		fmt.Printf("Current debug value: %v\n", debug)
	}

}
