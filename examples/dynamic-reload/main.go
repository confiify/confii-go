// Package main demonstrates file watching and dynamic reloading.
// Config automatically reloads when the underlying files change on disk.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	confii "github.com/qualitycoe/confii-go"
	"github.com/qualitycoe/confii-go/loader"
)

func main() {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("config.yaml")),
		confii.WithDynamicReloading(true), // enables fsnotify file watcher
	)
	if err != nil {
		log.Fatal(err)
	}

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

	// Stop watching when done
	cfg.StopWatching()
}
