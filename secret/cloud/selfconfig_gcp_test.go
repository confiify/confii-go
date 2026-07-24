// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build gcp

package cloud

import (
	"testing"

	confii "github.com/confiify/confii-go"
)

func TestGCPSelfConfigProviderRegistered(t *testing.T) {
	if _, ok := confii.LookupSelfConfigSecretProvider("gcp"); !ok {
		t.Fatal("gcp self-config secret provider was not registered")
	}
}
