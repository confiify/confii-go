// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package main demonstrates type-safe configuration access using Go generics.
// Config[T] decodes and validates the config into a strongly-typed struct.
package main

import (
	"fmt"
	"log"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
)

type AppConfig struct {
	Database DatabaseConfig `confii:"database"`
	Debug    bool           `confii:"debug"`
}

type DatabaseConfig struct {
	Host     string `confii:"host" validate:"required"`
	Port     int    `confii:"port" validate:"required,min=1,max=65535"`
	Name     string `confii:"name" validate:"required"`
	Password string `confii:"password"`
}

func main() {
	cfg, err := confii.New[AppConfig](confii.WithLoaders(loader.NewYAML("config.yaml")),
		confii.WithValidateOnLoad(true),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Type-safe access — IDE autocomplete works here
	model, err := cfg.Typed()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Host:", model.Database.Host)
	fmt.Println("Port:", model.Database.Port)
	fmt.Println("Name:", model.Database.Name)
	fmt.Println("Debug:", model.Debug)
}
