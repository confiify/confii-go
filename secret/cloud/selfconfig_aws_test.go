// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build aws

package cloud

import (
	"testing"

	confii "github.com/confiify/confii-go/v2"
)

func TestAWSSelfConfigProviderRegistered(t *testing.T) {
	if _, ok := confii.LookupSelfConfigSecretProvider("aws"); !ok {
		t.Fatal("aws self-config secret provider was not registered")
	}
}
