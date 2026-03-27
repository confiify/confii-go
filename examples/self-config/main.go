// Package main demonstrates Confii's self-configuration feature.
// Confii reads its own settings from a .confii.yaml file before any
// user loaders run. Settings are applied with 3-tier priority:
// explicit argument > self-config > built-in default.
//
// Search order: CWD (confii.*, .confii.*), then ~/.config/confii/
//
// See the .confii.yaml file in this directory for available settings.
package main

import (
	"context"
	"fmt"
	"log"

	confii "github.com/confiify/confii-go"
)

func main() {
	// With a .confii.yaml present, settings are auto-discovered.
	// No explicit options needed — they come from the self-config file.
	cfg, err := confii.New[any](context.Background())
	if err != nil {
		log.Fatal(err)
	}

	// The self-config set:
	//   default_environment: development
	//   env_prefix: APP
	//   deep_merge: true
	//   default_files: [config/base.yaml, config/dev.yaml]

	fmt.Println("Environment:", cfg.Env())
	fmt.Println("Keys:", cfg.Keys())
}
