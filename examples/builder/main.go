// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package main demonstrates the fluent builder pattern for constructing
// a Config instance with chained method calls.
package main

import (
	"context"
	"fmt"
	"log"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
)

type AppConfig struct {
	App struct {
		Name  string `confii:"name"`
		Debug bool   `confii:"debug"`
	} `confii:"app"`
	Database struct {
		Host string `confii:"host"`
		Port int    `confii:"port"`
	} `confii:"database"`
}

func main() {
	cfg, err := confii.NewBuilder[AppConfig]().
		WithEnv("production").
		AddLoader(loader.NewYAML("../environment/config.yaml")).
		WithMergeStrategy(confii.StrategyMerge).
		EnableFreezeOnLoad().
		BuildWithContext(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	model, err := cfg.Typed()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("App:", model.App.Name)
	fmt.Println("Frozen:", cfg.IsFrozen())
}
