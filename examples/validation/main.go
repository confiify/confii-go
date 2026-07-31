// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package main demonstrates both struct tag validation and JSON Schema
// validation for configuration.
package main

import (
	"fmt"
	"log"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/confiify/confii-go/v2/validate"
)

// Struct tag validation using go-playground/validator
type DBConfig struct {
	Host           string `confii:"host" validate:"required,hostname"`
	Port           int    `confii:"port" validate:"required,min=1,max=65535"`
	Name           string `confii:"name" validate:"required"`
	MaxConnections int    `confii:"max_connections" validate:"min=1,max=500"`
}

func main() {
	// --- Struct tag validation (validate on load) ---
	cfg, err := confii.New[DBConfig](confii.WithLoaders(loader.NewYAML("config.yaml")),
		confii.WithValidateOnLoad(true),
		confii.WithStrictValidation(true),
	)
	if err != nil {
		log.Fatal("Struct validation failed:", err)
	}

	model, err := cfg.Typed()
	if err != nil {
		log.Fatal("Typed decoding failed:", err)
	}
	fmt.Printf("Valid config: %s:%d/%s (max %d conns)\n",
		model.Host, model.Port, model.Name, model.MaxConnections)

	// --- JSON Schema validation ---
	v, err := validate.NewJSONSchemaValidatorFromFile("schema.json")
	if err != nil {
		log.Fatal("Schema load failed:", err)
	}

	data, err := cfg.ToDict()
	if err != nil {
		log.Fatal("Configuration materialization failed:", err)
	}
	if err := v.Validate(data); err != nil {
		log.Fatal("JSON Schema validation failed:", err)
	}
	fmt.Println("JSON Schema validation passed")
}
