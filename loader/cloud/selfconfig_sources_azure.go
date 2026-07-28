// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build azure

package cloud

import (
	"context"
	"fmt"

	confii "github.com/confiify/confii-go"
)

func init() {
	confii.RegisterSelfConfigSourceProvider("azure_blob", newSelfConfigAzureBlob)
	confii.RegisterSelfConfigSourceProvider("azure-blob", newSelfConfigAzureBlob)
}

func newSelfConfigAzureBlob(_ context.Context, cfg map[string]any) (confii.Loader, error) {
	containerURL := sourceString(cfg, "container_url", "url")
	blob := sourceString(cfg, "blob", "blob_name", "path")
	if containerURL == "" || blob == "" {
		return nil, fmt.Errorf("azure_blob source requires container_url and blob")
	}
	opts := make([]AzureBlobOption, 0, 1)
	switch {
	case sourceString(cfg, "connection_string") != "":
		opts = append(opts, WithAzureConnectionString(sourceString(cfg, "connection_string")))
	case sourceString(cfg, "account_key") != "":
		account := sourceString(cfg, "account_name", "account")
		if account == "" {
			return nil, fmt.Errorf("azure_blob account_key requires account_name")
		}
		opts = append(opts, WithAzureAccountKey(account, sourceString(cfg, "account_key")))
	case sourceString(cfg, "sas_token") != "":
		account := sourceString(cfg, "account_name", "account")
		if account == "" {
			return nil, fmt.Errorf("azure_blob sas_token requires account_name")
		}
		opts = append(opts, WithAzureSASToken(account, sourceString(cfg, "sas_token")))
	}
	return NewAzureBlob(containerURL, blob, opts...), nil
}
