// Package main demonstrates Hydra-style config composition using
// _include and _defaults directives. Included files are resolved
// relative to the source file, with cycle detection and max depth of 10.
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
		confii.WithLoaders(loader.NewYAML("app.yaml")),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Values from _defaults
	timeout, _ := cfg.Get("timeout")
	fmt.Println("Timeout:", timeout) // 30

	// Values from _include: shared/logging.yaml
	logLevel, _ := cfg.Get("logging.level")
	fmt.Println("Log level:", logLevel) // info

	// Values from _include: shared/database.yaml
	dbHost, _ := cfg.Get("database.host")
	fmt.Println("DB host:", dbHost) // localhost

	// Values from the main file
	appName, _ := cfg.Get("app.name")
	fmt.Println("App:", appName) // my-service
}
