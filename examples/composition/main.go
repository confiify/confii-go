// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package main demonstrates Hydra-style config composition using
// _include and _defaults directives. Included files are resolved
// relative to the source file, with cycle detection and max depth of 10.
package main

import (
	"fmt"
	"log"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
)

func main() {
	cfg, err := confii.New[any](confii.WithLoaders(loader.NewYAML("app.yaml")))
	if err != nil {
		log.Fatal(err)
	}

	// Values from _defaults
	timeout := cfg.MustGet("timeout")
	fmt.Println("Timeout:", timeout) // 30

	// Values from _include: shared/logging.yaml
	logLevel := cfg.MustGet("logging.level")
	fmt.Println("Log level:", logLevel) // info

	// Values from _include: shared/database.yaml
	dbHost := cfg.MustGet("database.host")
	fmt.Println("DB host:", dbHost) // localhost

	// Values from the main file
	appName := cfg.MustGet("app.name")
	fmt.Println("App:", appName) // my-service
}
