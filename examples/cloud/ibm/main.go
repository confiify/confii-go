//go:build ibm

// Package main demonstrates IBM Cloud Object Storage configuration loading.
package main

import (
	"context"
	"log"

	confii "github.com/confiify/confii-go"
	loadercloud "github.com/confiify/confii-go/loader/cloud"
)

func main() {
	cos := loadercloud.NewIBMCOS("my-config-bucket", "production/app.yaml")
	cfg, err := confii.New[any](context.Background(), confii.WithLoaders(cos))
	if err != nil {
		log.Fatal(err)
	}
	_, _ = cfg.Get("database.host")
}
