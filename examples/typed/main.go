// Package main demonstrates type-safe configuration access using Go generics.
// Config[T] decodes and validates the config into a strongly-typed struct.
package main

import (
	"context"
	"fmt"
	"log"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
)

type AppConfig struct {
	Database DatabaseConfig `mapstructure:"database"`
	Debug    bool           `mapstructure:"debug"`
}

type DatabaseConfig struct {
	Host     string `mapstructure:"host" validate:"required"`
	Port     int    `mapstructure:"port" validate:"required,min=1,max=65535"`
	Name     string `mapstructure:"name" validate:"required"`
	Password string `mapstructure:"password"`
}

func main() {
	cfg, err := confii.New[AppConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML("config.yaml")),
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
