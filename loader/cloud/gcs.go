// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build gcp

package cloud

import (
	"context"
	"fmt"
	"io"
	"os"

	"cloud.google.com/go/storage"
	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
	"google.golang.org/api/option"
)

// GCSLoader loads configuration from Google Cloud Storage.
type GCSLoader struct {
	bucketName      string
	blobName        string
	projectID       string
	credentialsPath string
}

// GCSOption configures a GCSLoader.
type GCSOption func(*GCSLoader)

// WithGCSProject sets the quota/billing project used by Application Default
// Credentials. GCS bucket names are global, so it does not alter the object
// resource path.
func WithGCSProject(id string) GCSOption {
	return func(l *GCSLoader) { l.projectID = id }
}

// WithGCSCredentials sets a service-account JSON file. When omitted, the
// Google Application Default Credentials chain is used.
func WithGCSCredentials(path string) GCSOption {
	return func(l *GCSLoader) { l.credentialsPath = path }
}

// NewGCS creates a loader for gs://bucket/blob. Defaults are read from
// GCP_PROJECT_ID and GOOGLE_APPLICATION_CREDENTIALS. Construction performs no
// network request; options are applied in order.
func NewGCS(bucket, blob string, opts ...GCSOption) *GCSLoader {
	l := &GCSLoader{
		bucketName:      bucket,
		blobName:        blob,
		projectID:       os.Getenv("GCP_PROJECT_ID"),
		credentialsPath: os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Source returns the identifier for this loader's configuration source.
func (l *GCSLoader) Source() string {
	return fmt.Sprintf("gs://%s/%s", l.bucketName, l.blobName)
}

// Load fetches and parses the object while honoring ctx. Format is selected
// from the blob extension and defaults to JSON when unknown. Client, read, and
// transport failures wrap [confii.ErrConfigLoad]; format failures wrap
// [confii.ErrConfigFormat]. The temporary storage client is closed before
// Load returns.
func (l *GCSLoader) Load(ctx context.Context) (map[string]any, error) {
	var clientOpts []option.ClientOption
	if l.credentialsPath != "" {
		// Google deprecated WithCredentialsFile over the risk of long-lived
		// service-account key files. Confii does not create that risk; it honors
		// a path the deployment chose to configure, and dropping support would
		// break every deployment that relies on it. Migrating to workload
		// identity is the deployment's decision, not this library's, so the call
		// stays and the warning is suppressed deliberately rather than by a
		// blanket linter exclusion.
		//nolint:staticcheck // SA1019: see above; removal is a breaking change.
		clientOpts = append(clientOpts, option.WithCredentialsFile(l.credentialsPath))
	}
	if l.projectID != "" {
		// GCS bucket names are globally unique, so the project is not part of
		// the object address. It is still meaningful as the quota/billing
		// project used by Application Default Credentials.
		clientOpts = append(clientOpts, option.WithQuotaProject(l.projectID))
	}

	client, err := storage.NewClient(ctx, clientOpts...)
	if err != nil {
		return nil, confii.NewLoadError(l.Source(), err)
	}
	// The client exists for this load only. Closing it releases connections;
	// a failure to do so cannot change the configuration already read.
	defer func() { _ = client.Close() }()

	reader, err := client.Bucket(l.bucketName).Object(l.blobName).NewReader(ctx)
	if err != nil {
		return nil, confii.NewLoadError(l.Source(), err)
	}
	// The body is fully read below; a close failure has no bearing on the
	// loaded configuration and no caller that could act on it.
	defer func() { _ = reader.Close() }()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, confii.NewLoadError(l.Source(), err)
	}

	format := loader.FormatFromExtension(l.blobName)
	if format == loader.FormatUnknown {
		format = loader.FormatJSON
	}

	return loader.ParseContent(data, format, l.Source())
}
