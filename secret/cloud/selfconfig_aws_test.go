//go:build aws

package cloud

import (
	"testing"

	confii "github.com/confiify/confii-go"
)

func TestAWSSelfConfigProviderRegistered(t *testing.T) {
	if _, ok := confii.LookupSelfConfigSecretProvider("aws"); !ok {
		t.Fatal("aws self-config secret provider was not registered")
	}
}
