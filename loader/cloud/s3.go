// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

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
	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
)

// S3Loader loads configuration from an AWS S3 object.
type S3Loader struct {
	s3URL     string
	region    string
	accessKey string
	secretKey string
	endpoint  string
	pathStyle bool
	bucket    string
	key       string
}

// S3Option configures an S3Loader.
type S3Option func(*S3Loader)

// WithS3Region sets the AWS region. NewS3 otherwise uses AWS_DEFAULT_REGION or
// us-east-1.
func WithS3Region(region string) S3Option {
	return func(l *S3Loader) { l.region = region }
}

// WithS3Credentials sets static AWS credentials. When omitted, the standard
// AWS SDK credential chain is used. Do not embed credentials in source code.
func WithS3Credentials(accessKey, secretKey string) S3Option {
	return func(l *S3Loader) {
		l.accessKey = accessKey
		l.secretKey = secretKey
	}
}

// WithS3Endpoint sets a custom S3 API endpoint, such as LocalStack.
func WithS3Endpoint(endpoint string) S3Option {
	return func(l *S3Loader) { l.endpoint = strings.TrimSpace(endpoint) }
}

// WithS3PathStyle uses path-style bucket addressing. This is commonly needed
// by local emulators and S3-compatible services that do not provide wildcard
// DNS for virtual-hosted bucket names.
func WithS3PathStyle(enabled bool) S3Option {
	return func(l *S3Loader) { l.pathStyle = enabled }
}

// NewS3 creates a loader from s3://bucket/object-key. It validates the scheme
// but performs no network request; missing buckets, keys, and credentials are
// reported by Load. Options are applied in order.
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

// Source returns the identifier for this loader's configuration source.
func (l *S3Loader) Source() string { return l.s3URL }

// Load fetches and parses the configured object. Format is selected from the
// object-key extension; an unknown extension is treated as JSON. AWS and body
// read failures wrap [confii.ErrConfigLoad], while parsing failures wrap
// [confii.ErrConfigFormat]. The request honors ctx.
func (l *S3Loader) Load(ctx context.Context) (map[string]any, error) {
	cfg, err := l.awsConfig(ctx)
	if err != nil {
		return nil, confii.NewLoadError(l.s3URL, err)
	}

	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		if l.endpoint != "" {
			options.BaseEndpoint = aws.String(l.endpoint)
		}
		options.UsePathStyle = l.pathStyle
	})
	output, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(l.bucket),
		Key:    aws.String(l.key),
	})
	if err != nil {
		return nil, confii.NewLoadError(l.s3URL, err)
	}
	// The body is fully read below; a close failure has no bearing on the
	// loaded configuration and no caller that could act on it.
	defer func() { _ = output.Body.Close() }()

	data, err := io.ReadAll(output.Body)
	if err != nil {
		return nil, confii.NewLoadError(l.s3URL, err)
	}

	format := loader.FormatFromExtension(l.key)
	if format == loader.FormatUnknown {
		format = loader.FormatJSON
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
