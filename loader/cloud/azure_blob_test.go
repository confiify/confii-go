//go:build azure

package cloud

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAzureContainerURL(t *testing.T) {
	service, container, sas, err := parseAzureContainerURL("https://account.blob.core.windows.net/configs?sv=2026&sig=secret")
	require.NoError(t, err)
	assert.Equal(t, "https://account.blob.core.windows.net", service)
	assert.Equal(t, "configs", container)
	assert.Equal(t, "sv=2026&sig=secret", sas)
}

func TestParseAzureContainerURLRejectsBlobPath(t *testing.T) {
	_, _, _, err := parseAzureContainerURL("https://account.blob.core.windows.net/configs/app.yaml")
	require.Error(t, err)
}

func TestNewAzureBlobSeparatesServiceAndContainer(t *testing.T) {
	l := NewAzureBlob("https://account.blob.core.windows.net/configs", "app/config.yaml")
	assert.Equal(t, "https://account.blob.core.windows.net", l.serviceURL)
	assert.Equal(t, "configs", l.containerName)
}
