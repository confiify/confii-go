// Package main demonstrates basic Confii usage: loading a YAML config
// and accessing values using dot-notation key paths.
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
		confii.WithLoaders(
			loader.NewYAML("config.yaml"),
		),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Untyped access
	name, _ := cfg.Get("app.name")
	fmt.Println("App:", name)

	// Typed getters with defaults
	port := cfg.GetIntOr("database.port", 3306)
	fmt.Println("Port:", port)

	debug := cfg.GetBoolOr("debug", false)
	fmt.Println("Debug:", debug)

	// Check existence
	fmt.Println("Has cache?", cfg.Has("cache"))

	// List all keys
	keys := cfg.Keys()
	fmt.Println("Keys:", keys)
}
