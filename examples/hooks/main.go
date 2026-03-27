// Package main demonstrates the hook system. Confii supports 4 hook types:
// key hooks, value hooks, condition hooks, and global hooks.
// Hooks transform values when they are accessed.
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
)

func main() {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("config.yaml")),
		confii.WithEnvExpander(true), // built-in ${VAR} expansion hook
	)
	if err != nil {
		log.Fatal(err)
	}

	hp := cfg.HookProcessor()

	// Key hook: fires when accessing a specific key
	hp.RegisterKeyHook("app.name", func(key string, value any) any {
		return strings.ToUpper(value.(string))
	})

	// Condition hook: fires when condition is met
	hp.RegisterConditionHook(
		func(key string, value any) bool {
			return strings.HasPrefix(key, "feature.")
		},
		func(key string, value any) any {
			fmt.Printf("  [hook] Feature flag accessed: %s\n", key)
			return value
		},
	)

	// Global hook: fires for every value
	hp.RegisterGlobalHook(func(key string, value any) any {
		// Example: mask sensitive values in logs
		if strings.Contains(key, "password") || strings.Contains(key, "secret") {
			return "****"
		}
		return value
	})

	name, _ := cfg.Get("app.name")
	fmt.Println("App name:", name) // MY-SERVICE (uppercased by key hook)
}
