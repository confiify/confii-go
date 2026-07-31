// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build ibm

// Package main demonstrates IBM Cloud Object Storage configuration loading.
package main

import (
	"log"

	loadercloud "github.com/confiify/confii-go/loader/cloud/v2"
	confii "github.com/confiify/confii-go/v2"
)

func main() {
	cos := loadercloud.NewIBMCOS("my-config-bucket", "production/app.yaml")
	cfg, err := confii.New[any](confii.WithLoaders(cos))
	if err != nil {
		log.Fatal(err)
	}
	if _, err := cfg.Get("database.host"); err != nil {
		log.Fatal(err)
	}
}
