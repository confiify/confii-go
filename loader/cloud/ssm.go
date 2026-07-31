// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build aws

package cloud

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/confiify/confii-go/loader/cloud/v2/internal/cloudutil"
	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/configmap"
)

// SSMLoader loads configuration from AWS Systems Manager Parameter Store.
type SSMLoader struct {
	pathPrefix string
	decrypt    bool
	region     string
	accessKey  string
	secretKey  string
	endpoint   string
}

// SSMOption configures an SSMLoader.
type SSMOption func(*SSMLoader)

// WithSSMDecrypt controls parameter decryption (default true).
func WithSSMDecrypt(v bool) SSMOption {
	return func(l *SSMLoader) { l.decrypt = v }
}

// WithSSMRegion sets the AWS region.
func WithSSMRegion(region string) SSMOption {
	return func(l *SSMLoader) { l.region = region }
}

// WithSSMCredentials sets explicit AWS credentials.
func WithSSMCredentials(accessKey, secretKey string) SSMOption {
	return func(l *SSMLoader) {
		l.accessKey = accessKey
		l.secretKey = secretKey
	}
}

// WithSSMEndpoint sets a custom SSM API endpoint, such as LocalStack.
func WithSSMEndpoint(endpoint string) SSMOption {
	return func(l *SSMLoader) { l.endpoint = strings.TrimSpace(endpoint) }
}

// NewSSM creates an SSM Parameter Store loader rooted at pathPrefix. A trailing
// slash is added when absent. The default region is AWS_DEFAULT_REGION or
// us-east-1, and secure-string decryption is enabled. Construction performs no
// network request.
func NewSSM(pathPrefix string, opts ...SSMOption) *SSMLoader {
	if !strings.HasSuffix(pathPrefix, "/") {
		pathPrefix += "/"
	}
	l := &SSMLoader{
		pathPrefix: pathPrefix,
		decrypt:    true,
		region:     envOrDefault("AWS_DEFAULT_REGION", "us-east-1"),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Source returns the identifier for this loader's configuration source.
func (l *SSMLoader) Source() string { return "ssm:" + l.pathPrefix }

// Load recursively fetches parameters beneath the prefix, removes that prefix,
// converts remaining slash-separated names to dot-separated configuration
// paths, and conservatively parses scalar values. It follows pagination and
// returns nil, nil when no parameters exist. Invalid overlapping paths and AWS
// failures wrap [confii.ErrConfigLoad]. The request honors ctx.
func (l *SSMLoader) Load(ctx context.Context) (map[string]any, error) {
	cfg, err := l.awsConfig(ctx)
	if err != nil {
		return nil, confii.NewLoadError(l.Source(), err)
	}

	client := ssm.NewFromConfig(cfg, func(options *ssm.Options) {
		if l.endpoint != "" {
			options.BaseEndpoint = aws.String(l.endpoint)
		}
	})
	result := make(map[string]any)

	var nextToken *string
	for {
		output, err := client.GetParametersByPath(ctx, &ssm.GetParametersByPathInput{
			Path:           aws.String(l.pathPrefix),
			Recursive:      aws.Bool(true),
			WithDecryption: aws.Bool(l.decrypt),
			NextToken:      nextToken,
		})
		if err != nil {
			return nil, confii.NewLoadError(l.Source(), err)
		}

		for _, param := range output.Parameters {
			// Strip prefix from parameter name.
			name := strings.TrimPrefix(aws.ToString(param.Name), l.pathPrefix)
			// Split on / to create nested keys.
			parts := strings.Split(name, "/")
			keyPath := strings.Join(parts, ".")
			value := cloudutil.ParseScalar(aws.ToString(param.Value))
			if err := configmap.Set(result, keyPath, value); err != nil {
				return nil, confii.NewLoadError(l.Source(), err)
			}
		}

		nextToken = output.NextToken
		if nextToken == nil {
			break
		}
	}

	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func (l *SSMLoader) awsConfig(ctx context.Context) (aws.Config, error) {
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
