// Package main demonstrates environment-aware configuration.
// Confii merges the "default" section with the active environment section.
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
		confii.WithLoaders(loader.NewYAML("config.yaml")),
		confii.WithEnv("production"),
	)
	if err != nil {
		log.Fatal(err)
	}

	host, _ := cfg.Get("database.host")
	port := cfg.GetIntOr("database.port", 0)
	debug := cfg.GetBoolOr("app.debug", true)

	fmt.Println("Host:", host)   // prod-db.example.com (from production)
	fmt.Println("Port:", port)   // 5432 (inherited from default)
	fmt.Println("Debug:", debug) // false (from production)

	// Or read environment from an OS variable:
	// confii.WithEnvSwitcher("APP_ENV")
}
