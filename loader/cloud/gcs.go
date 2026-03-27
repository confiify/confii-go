//go:build gcp

package cloud

import (
	"context"
	"fmt"
	"io"
	"os"

	"cloud.google.com/go/storage"
	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/internal/formatparse"
	"github.com/confiify/confii-go/loader"
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

// WithGCSProject sets the GCP project ID.
func WithGCSProject(id string) GCSOption {
	return func(l *GCSLoader) { l.projectID = id }
}

// WithGCSCredentials sets the path to a service account key file.
func WithGCSCredentials(path string) GCSOption {
	return func(l *GCSLoader) { l.credentialsPath = path }
}

// NewGCS creates a new Google Cloud Storage loader.
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

// Load fetches configuration from the Google Cloud Storage object at the configured bucket and blob path.
func (l *GCSLoader) Load(ctx context.Context) (map[string]any, error) {
	var clientOpts []option.ClientOption
	if l.credentialsPath != "" {
		clientOpts = append(clientOpts, option.WithCredentialsFile(l.credentialsPath))
	}

	client, err := storage.NewClient(ctx, clientOpts...)
	if err != nil {
		return nil, confii.NewLoadError(l.Source(), err)
	}
	defer client.Close()

	reader, err := client.Bucket(l.bucketName).Object(l.blobName).NewReader(ctx)
	if err != nil {
		return nil, confii.NewLoadError(l.Source(), err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, confii.NewLoadError(l.Source(), err)
	}

	format := formatparse.FromExtension(l.blobName)
	if format == formatparse.FormatUnknown {
		format = formatparse.FormatJSON
	}

	return loader.ParseContent(data, format, l.Source())
}
