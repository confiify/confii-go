// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build azure

package cloud

import (
	"testing"

	confii "github.com/confiify/confii-go/v2"
)

func TestAzureSelfConfigProviderRegistered(t *testing.T) {
	if _, ok := confii.LookupSelfConfigSecretProvider("azure"); !ok {
		t.Fatal("azure self-config secret provider was not registered")
	}
}
