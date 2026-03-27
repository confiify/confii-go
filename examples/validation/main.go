// Package main demonstrates both struct tag validation and JSON Schema
// validation for configuration.
package main

import (
	"context"
	"fmt"
	"log"

	confii "github.com/qualitycoe/confii-go"
	"github.com/qualitycoe/confii-go/loader"
	"github.com/qualitycoe/confii-go/validate"
)

// Struct tag validation using go-playground/validator
type DBConfig struct {
	Host           string `mapstructure:"host" validate:"required,hostname"`
	Port           int    `mapstructure:"port" validate:"required,min=1,max=65535"`
	Name           string `mapstructure:"name" validate:"required"`
	MaxConnections int    `mapstructure:"max_connections" validate:"min=1,max=500"`
}

func main() {
	// --- Struct tag validation (validate on load) ---
	cfg, err := confii.New[DBConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML("config.yaml")),
		confii.WithValidateOnLoad(true),
		confii.WithStrictValidation(true),
	)
	if err != nil {
		log.Fatal("Struct validation failed:", err)
	}

	model, _ := cfg.Typed()
	fmt.Printf("Valid config: %s:%d/%s (max %d conns)\n",
		model.Host, model.Port, model.Name, model.MaxConnections)

	// --- JSON Schema validation ---
	v, err := validate.NewJSONSchemaValidatorFromFile("schema.json")
	if err != nil {
		log.Fatal("Schema load failed:", err)
	}

	if err := v.Validate(cfg.ToDict()); err != nil {
		log.Fatal("JSON Schema validation failed:", err)
	}
	fmt.Println("JSON Schema validation passed")
}
