//go:build aws

// Package cloud provides cloud-based configuration loaders behind build tags.
package cloud

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	confii "github.com/qualitycoe/confii-go"
	"github.com/qualitycoe/confii-go/internal/formatparse"
	"github.com/qualitycoe/confii-go/loader"
)

// S3Loader loads configuration from an AWS S3 object.
type S3Loader struct {
	s3URL        string
	region       string
	accessKey    string
	secretKey    string
	bucket       string
	key          string
}

// S3Option configures an S3Loader.
type S3Option func(*S3Loader)

// WithS3Region sets the AWS region.
func WithS3Region(region string) S3Option {
	return func(l *S3Loader) { l.region = region }
}

// WithS3Credentials sets explicit AWS credentials.
func WithS3Credentials(accessKey, secretKey string) S3Option {
	return func(l *S3Loader) {
		l.accessKey = accessKey
		l.secretKey = secretKey
	}
}

// NewS3 creates a new S3 loader from an s3:// URL.
func NewS3(s3URL string, opts ...S3Option) (*S3Loader, error) {
	parsed, err := url.Parse(s3URL)
	if err != nil {
		return nil, fmt.Errorf("invalid S3 URL: %w", err)
	}
	if parsed.Scheme != "s3" {
		return nil, fmt.Errorf("expected s3:// scheme, got %s://", parsed.Scheme)
	}

	l := &S3Loader{
		s3URL:  s3URL,
		region: envOrDefault("AWS_DEFAULT_REGION", "us-east-1"),
		bucket: parsed.Host,
		key:    strings.TrimPrefix(parsed.Path, "/"),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l, nil
}

func (l *S3Loader) Source() string { return l.s3URL }

func (l *S3Loader) Load(ctx context.Context) (map[string]any, error) {
	cfg, err := l.awsConfig(ctx)
	if err != nil {
		return nil, confii.NewLoadError(l.s3URL, err)
	}

	client := s3.NewFromConfig(cfg)
	output, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(l.bucket),
		Key:    aws.String(l.key),
	})
	if err != nil {
		return nil, confii.NewLoadError(l.s3URL, err)
	}
	defer output.Body.Close()

	data, err := io.ReadAll(output.Body)
	if err != nil {
		return nil, confii.NewLoadError(l.s3URL, err)
	}

	format := formatparse.FromExtension(l.key)
	if format == formatparse.FormatUnknown {
		format = formatparse.FormatJSON
	}

	return loader.ParseContent(data, format, l.s3URL)
}

func (l *S3Loader) awsConfig(ctx context.Context) (aws.Config, error) {
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(l.region),
	}
	if l.accessKey != "" && l.secretKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(l.accessKey, l.secretKey, ""),
		))
	}
	return config.LoadDefaultConfig(ctx, opts...)
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
