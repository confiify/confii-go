// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package main demonstrates Confii's self-configuration feature.
// Confii reads its own settings from a .confii.yaml file before any
// user loaders run. Settings are applied with 3-tier priority:
// explicit argument > self-config > built-in default.
//
// Search order: CWD (confii.*, .confii.*), then ~/.config/confii/
//
// Run from this directory:
//
//	go run .
//	APP_ENV=production go run .
//	APP_SERVER__PORT=9090 go run .
package main

import (
	"fmt"
	"log"

	confii "github.com/confiify/confii-go/v2"
)

func main() {
	// With a .confii.yaml present, settings are auto-discovered.
	// No explicit options needed — they come from the self-config file.
	cfg, err := confii.New[any]()
	if err != nil {
		log.Fatal(err)
	}

	// The self-config set:
	//   default_environment: development
	//   env_prefix: APP
	//   environment_strategy: named_files
	//   sources: [config/default.yaml, config/{environment}.yaml]
	//   env_prefix: APP (OS variables are the final override layer)

	fmt.Println("Environment:", cfg.Env())
	fmt.Println("Application:", cfg.GetStringOr("app.name", "unknown"))
	fmt.Printf("Server: %s:%d\n",
		cfg.GetStringOr("server.host", "unknown"),
		cfg.GetIntOr("server.port", 0),
	)
	fmt.Println("Database:", cfg.GetStringOr("database.host", "unknown"))
}
