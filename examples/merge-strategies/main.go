// Package main demonstrates advanced merge strategies.
// Different sections can use different strategies: replace, merge, append,
// prepend, intersection, or union.
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
			loader.NewYAML("base.yaml"),
			loader.NewYAML("overlay.yaml"),
		),
		confii.WithMergeStrategyOption(confii.StrategyMerge),
		confii.WithMergeStrategyMap(map[string]confii.MergeStrategy{
			"database": confii.StrategyReplace,  // replace entire database section
			"features": confii.StrategyAppend,   // append new features to the list
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// database section is fully replaced by overlay
	host, _ := cfg.Get("database.host")
	fmt.Println("Host:", host) // prod-db.example.com

	// features list is appended
	features, _ := cfg.Get("features")
	fmt.Println("Features:", features) // [auth logging monitoring tracing]
}
