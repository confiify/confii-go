// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package main demonstrates advanced merge strategies.
// Different sections can use different strategies: replace, shallow merge,
// deep merge, append, prepend, intersection, or union.
package main

import (
	"fmt"
	"log"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
)

func main() {
	cfg, err := confii.New[any](confii.WithLoaders(
		loader.NewYAML("base.yaml"),
		loader.NewYAML("overlay.yaml"),
	),
		confii.WithMergeStrategy(confii.StrategyMerge),
		confii.WithMergeStrategyMap(map[string]confii.MergeStrategy{
			"database": confii.StrategyReplace, // replace entire database section
			"features": confii.StrategyAppend,  // append new features to the list
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// database section is fully replaced by overlay
	host := cfg.MustGet("database.host")
	fmt.Println("Host:", host) // prod-db.example.com

	// features list is appended
	features := cfg.MustGet("features")
	fmt.Println("Features:", features) // [auth logging monitoring tracing]
}
