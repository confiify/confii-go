// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package main demonstrates the hook system. Confii supports 4 hook types:
// key hooks, value hooks, condition hooks, and global hooks.
// Hooks transform leaf values when Confii prepares a read, including the map
// traversal performed before Typed decodes a struct.
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
)

type appConfig struct {
	App struct {
		Name string `confii:"name"`
	} `confii:"app"`
}

func main() {
	cfg, err := confii.New[appConfig](
		confii.WithLoaders(loader.NewYAML("config.yaml")),
		confii.WithEnvExpander(true),
		// Hooks are frozen into the construction plan and run while Confii
		// materializes the configuration snapshot.
		confii.WithKeyHook("app.name", func(_ context.Context, _ string, value any) (any, error) {
			return strings.ToUpper(value.(string)), nil
		}),
		confii.WithConditionHook(
			func(_ context.Context, key string, _ any) (bool, error) {
				return strings.HasPrefix(key, "feature."), nil
			},
			func(_ context.Context, key string, value any) (any, error) {
				fmt.Printf("  [hook] Materializing feature flag: %s\n", key)
				return value, nil
			},
		),
		confii.WithGlobalHook(func(_ context.Context, key string, value any) (any, error) {
			if strings.Contains(key, "password") || strings.Contains(key, "secret") {
				return "****", nil
			}
			return value, nil
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	name, _ := cfg.Get("app.name")
	fmt.Println("App name:", name) // MY-SERVICE (uppercased by key hook)

	// Typed and Get observe the same already-materialized snapshot.
	typed, err := cfg.Typed()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Typed app name:", typed.App.Name) // MY-SERVICE
}
