//go:build azure

package cloud

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	confii "github.com/qualitycoe/confii-go"
	"github.com/qualitycoe/confii-go/internal/formatparse"
	"github.com/qualitycoe/confii-go/loader"
)

// AzureBlobLoader loads configuration from Azure Blob Storage.
type AzureBlobLoader struct {
	containerURL     string
	blobName         string
	accountName      string
	accountKey       string
	sasToken         string
	connectionString string
}

// AzureBlobOption configures an AzureBlobLoader.
type AzureBlobOption func(*AzureBlobLoader)

// WithAzureAccountKey sets account name and key.
func WithAzureAccountKey(name, key string) AzureBlobOption {
	return func(l *AzureBlobLoader) {
		l.accountName = name
		l.accountKey = key
	}
}

// WithAzureSASToken sets account name and SAS token.
func WithAzureSASToken(name, token string) AzureBlobOption {
	return func(l *AzureBlobLoader) {
		l.accountName = name
		l.sasToken = token
	}
}

// WithAzureConnectionString sets the connection string.
func WithAzureConnectionString(cs string) AzureBlobOption {
	return func(l *AzureBlobLoader) { l.connectionString = cs }
}

// NewAzureBlob creates a new Azure Blob Storage loader.
func NewAzureBlob(containerURL, blobName string, opts ...AzureBlobOption) *AzureBlobLoader {
	l := &AzureBlobLoader{
		containerURL:     containerURL,
		blobName:         blobName,
		accountName:      os.Getenv("AZURE_STORAGE_ACCOUNT"),
		accountKey:       os.Getenv("AZURE_STORAGE_KEY"),
		sasToken:         os.Getenv("AZURE_SAS_TOKEN"),
		connectionString: os.Getenv("AZURE_STORAGE_CONNECTION_STRING"),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

func (l *AzureBlobLoader) Source() string {
	return fmt.Sprintf("azure://%s/%s", l.containerURL, l.blobName)
}

func (l *AzureBlobLoader) Load(ctx context.Context) (map[string]any, error) {
	client, err := l.createClient()
	if err != nil {
		return nil, confii.NewLoadError(l.Source(), err)
	}

	resp, err := client.DownloadStream(ctx, l.containerURL, l.blobName, nil)
	if err != nil {
		return nil, confii.NewLoadError(l.Source(), err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, confii.NewLoadError(l.Source(), err)
	}

	format := formatparse.FromExtension(l.blobName)
	if format == formatparse.FormatUnknown {
		format = formatparse.FormatJSON
	}

	return loader.ParseContent(data, format, l.Source())
}

func (l *AzureBlobLoader) createClient() (*azblob.Client, error) {
	// Priority: connection_string > account+key > account+sas > default credential.
	if l.connectionString != "" {
		return azblob.NewClientFromConnectionString(l.connectionString, nil)
	}

	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net", l.accountName)

	if l.accountName != "" && l.accountKey != "" {
		cred, err := azblob.NewSharedKeyCredential(l.accountName, l.accountKey)
		if err != nil {
			return nil, err
		}
		return azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
	}

	if l.accountName != "" && l.sasToken != "" {
		urlWithSAS := serviceURL + "?" + l.sasToken
		return azblob.NewClientWithNoCredential(urlWithSAS, nil)
	}

	// Default credential (managed identity).
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("azure default credential: %w", err)
	}
	return azblob.NewClient(serviceURL, cred, nil)
}
