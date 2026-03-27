// Package main demonstrates loading configuration from multiple sources.
// Later loaders override earlier ones. Environment variables can also be used.
package main

import (
	"context"
	"fmt"
	"log"

	confii "github.com/qualitycoe/confii-go"
	"github.com/qualitycoe/confii-go/loader"
)

func main() {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(
			loader.NewYAML("defaults.yaml"),      // base config
			loader.NewYAML("overrides.yaml"),      // overrides base values
			loader.NewEnvironment("APP"),           // APP_DATABASE__HOST overrides further
		),
		confii.WithDeepMerge(true),
	)
	if err != nil {
		log.Fatal(err)
	}

	host, _ := cfg.Get("database.host")
	ssl, _ := cfg.Get("database.ssl")
	ttl, _ := cfg.Get("cache.ttl")

	fmt.Println("Host:", host)   // prod-db.example.com (from overrides.yaml)
	fmt.Println("SSL:", ssl)     // true (added by overrides.yaml)
	fmt.Println("TTL:", ttl)     // 3600 (overridden by overrides.yaml)
}
