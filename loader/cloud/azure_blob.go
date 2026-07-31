// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build azure

package cloud

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
)

// AzureBlobLoader loads configuration from Azure Blob Storage.
type AzureBlobLoader struct {
	containerURL     string
	containerName    string
	serviceURL       string
	embeddedSAS      string
	addressError     error
	blobName         string
	accountName      string
	accountKey       string
	sasToken         string
	connectionString string
}

// AzureBlobOption configures an AzureBlobLoader.
type AzureBlobOption func(*AzureBlobLoader)

// WithAzureAccountKey configures Shared Key authentication.
func WithAzureAccountKey(name, key string) AzureBlobOption {
	return func(l *AzureBlobLoader) {
		l.accountName = name
		l.accountKey = key
	}
}

// WithAzureSASToken configures an account name and SAS token. The token may be
// supplied with or without a leading question mark.
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

// NewAzureBlob creates a loader for blobName in containerURL. Authentication
// precedence is connection string, Shared Key, explicit SAS, SAS embedded in
// the URL, then DefaultAzureCredential. Environment defaults use
// AZURE_STORAGE_ACCOUNT, AZURE_STORAGE_KEY, AZURE_SAS_TOKEN, and
// AZURE_STORAGE_CONNECTION_STRING. Address errors are deferred to Load.
func NewAzureBlob(containerURL, blobName string, opts ...AzureBlobOption) *AzureBlobLoader {
	serviceURL, containerName, embeddedSAS, addressErr := parseAzureContainerURL(containerURL)
	l := &AzureBlobLoader{
		containerURL:     containerURL,
		containerName:    containerName,
		serviceURL:       serviceURL,
		embeddedSAS:      embeddedSAS,
		addressError:     addressErr,
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

// Source returns the identifier for this loader's configuration source.
func (l *AzureBlobLoader) Source() string {
	return fmt.Sprintf("azure://%s/%s", l.containerURL, l.blobName)
}

// Load downloads and parses the blob while honoring ctx. Format is selected
// from blobName and defaults to JSON. Address, credential, transport, and read
// failures wrap [confii.ErrConfigLoad]; parse failures wrap
// [confii.ErrConfigFormat].
func (l *AzureBlobLoader) Load(ctx context.Context) (map[string]any, error) {
	if l.addressError != nil {
		return nil, confii.NewLoadError(l.Source(), l.addressError)
	}
	client, err := l.createClient()
	if err != nil {
		return nil, confii.NewLoadError(l.Source(), err)
	}

	resp, err := client.DownloadStream(ctx, l.containerName, l.blobName, nil)
	if err != nil {
		return nil, confii.NewLoadError(l.Source(), err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, confii.NewLoadError(l.Source(), err)
	}

	format := loader.FormatFromExtension(l.blobName)
	if format == loader.FormatUnknown {
		format = loader.FormatJSON
	}

	return loader.ParseContent(data, format, l.Source())
}

func (l *AzureBlobLoader) createClient() (*azblob.Client, error) {
	// Priority: connection_string > account+key > account+sas > default credential.
	if l.connectionString != "" {
		return azblob.NewClientFromConnectionString(l.connectionString, nil)
	}

	serviceURL := l.serviceURL

	if l.accountName != "" && l.accountKey != "" {
		cred, err := azblob.NewSharedKeyCredential(l.accountName, l.accountKey)
		if err != nil {
			return nil, err
		}
		return azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
	}

	if l.accountName != "" && l.sasToken != "" {
		urlWithSAS := serviceURL + "?" + strings.TrimPrefix(l.sasToken, "?")
		return azblob.NewClientWithNoCredential(urlWithSAS, nil)
	}
	if l.embeddedSAS != "" {
		return azblob.NewClientWithNoCredential(serviceURL+"?"+l.embeddedSAS, nil)
	}

	// Default credential (managed identity).
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("azure default credential: %w", err)
	}
	return azblob.NewClient(serviceURL, cred, nil)
}

func parseAzureContainerURL(raw string) (serviceURL, containerName, sas string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", "", fmt.Errorf("invalid Azure container URL %q", raw)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", "", "", fmt.Errorf("Azure container URL must use http or https")
	}
	path := strings.Trim(u.EscapedPath(), "/")
	if path == "" || strings.Contains(path, "/") {
		return "", "", "", fmt.Errorf("Azure container URL must identify exactly one container")
	}
	containerName, err = url.PathUnescape(path)
	if err != nil || containerName == "" {
		return "", "", "", fmt.Errorf("invalid Azure container name in URL")
	}
	return u.Scheme + "://" + u.Host, containerName, u.RawQuery, nil
}
