// Package main demonstrates the fluent builder pattern for constructing
// a Config instance with chained method calls.
package main

import (
	"context"
	"fmt"
	"log"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
)

type AppConfig struct {
	App struct {
		Name  string `mapstructure:"name"`
		Debug bool   `mapstructure:"debug"`
	} `mapstructure:"app"`
	Database struct {
		Host string `mapstructure:"host"`
		Port int    `mapstructure:"port"`
	} `mapstructure:"database"`
}

func main() {
	cfg, err := confii.NewBuilder[AppConfig]().
		WithEnv("production").
		AddLoader(loader.NewYAML("../environment/config.yaml")).
		EnableDeepMerge().
		EnableFreezeOnLoad().
		Build(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	model, _ := cfg.Typed()
	fmt.Println("App:", model.App.Name)
	fmt.Println("Frozen:", cfg.IsFrozen())
}
