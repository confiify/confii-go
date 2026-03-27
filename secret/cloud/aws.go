//go:build aws

// Package cloud provides cloud-based secret store implementations behind build tags.
package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	confii "github.com/qualitycoe/confii-go"
)

// AWSSecretsManager implements SecretStore for AWS Secrets Manager.
type AWSSecretsManager struct {
	client *secretsmanager.Client
}

// AWSSecretsManagerOption configures AWSSecretsManager.
type AWSSecretsManagerOption func(*awsSMConfig)

type awsSMConfig struct {
	Region       string
	AccessKey    string
	SecretKey    string
	SessionToken string
	EndpointURL  string
}

// WithAWSRegion sets the AWS region.
func WithAWSRegion(region string) AWSSecretsManagerOption {
	return func(c *awsSMConfig) { c.Region = region }
}

// WithAWSCredentials sets explicit AWS credentials.
func WithAWSCredentials(accessKey, secretKey, sessionToken string) AWSSecretsManagerOption {
	return func(c *awsSMConfig) {
		c.AccessKey = accessKey
		c.SecretKey = secretKey
		c.SessionToken = sessionToken
	}
}

// WithAWSEndpoint sets a custom endpoint (e.g., LocalStack).
func WithAWSEndpoint(url string) AWSSecretsManagerOption {
	return func(c *awsSMConfig) { c.EndpointURL = url }
}

// NewAWSSecretsManager creates a new AWS Secrets Manager store.
func NewAWSSecretsManager(ctx context.Context, opts ...AWSSecretsManagerOption) (*AWSSecretsManager, error) {
	cfg := &awsSMConfig{Region: "us-east-1"}
	for _, opt := range opts {
		opt(cfg)
	}

	loadOpts := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
	}
	if cfg.AccessKey != "" {
		loadOpts = append(loadOpts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, cfg.SessionToken),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}

	var smOpts []func(*secretsmanager.Options)
	if cfg.EndpointURL != "" {
		smOpts = append(smOpts, func(o *secretsmanager.Options) {
			o.BaseEndpoint = aws.String(cfg.EndpointURL)
		})
	}

	client := secretsmanager.NewFromConfig(awsCfg, smOpts...)
	return &AWSSecretsManager{client: client}, nil
}

func (s *AWSSecretsManager) GetSecret(ctx context.Context, key string, opts ...confii.SecretOption) (any, error) {
	o := confii.ResolveSecretOptions(opts...)

	// Support "secret_name:json_key" syntax.
	secretName, jsonKey, _ := strings.Cut(key, ":")

	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretName),
	}

	// Version handling: stage names vs version IDs.
	if o.Version != "" {
		switch o.Version {
		case "AWSCURRENT", "AWSPENDING", "AWSPREVIOUS":
			input.VersionStage = aws.String(o.Version)
		default:
			input.VersionId = aws.String(o.Version)
		}
	}

	output, err := s.client.GetSecretValue(ctx, input)
	if err != nil {
		if isAWSNotFound(err) {
			return nil, fmt.Errorf("%w: %s", confii.ErrSecretNotFound, key)
		}
		return nil, fmt.Errorf("%w: %v", confii.ErrSecretAccess, err)
	}

	var value any
	if output.SecretString != nil {
		secretStr := aws.ToString(output.SecretString)
		// Try to parse as JSON.
		var parsed map[string]any
		if json.Unmarshal([]byte(secretStr), &parsed) == nil {
			value = parsed
		} else {
			value = secretStr
		}
	} else if output.SecretBinary != nil {
		value = output.SecretBinary
	}

	// Extract JSON key if specified.
	if jsonKey != "" {
		if m, ok := value.(map[string]any); ok {
			v, exists := m[jsonKey]
			if !exists {
				return nil, fmt.Errorf("%w: JSON key %q not found in secret %s", confii.ErrSecretValidation, jsonKey, secretName)
			}
			return v, nil
		}
	}

	return value, nil
}

func (s *AWSSecretsManager) SetSecret(ctx context.Context, key string, value any, _ ...confii.SecretOption) error {
	var secretStr string
	switch v := value.(type) {
	case string:
		secretStr = v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		secretStr = string(data)
	}

	// Try to update first, create if it doesn't exist.
	_, err := s.client.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
		SecretId:     aws.String(key),
		SecretString: aws.String(secretStr),
	})
	if err != nil {
		if isAWSNotFound(err) {
			_, err = s.client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{
				Name:         aws.String(key),
				SecretString: aws.String(secretStr),
			})
		}
	}
	return err
}

func (s *AWSSecretsManager) DeleteSecret(ctx context.Context, key string, _ ...confii.SecretOption) error {
	_, err := s.client.DeleteSecret(ctx, &secretsmanager.DeleteSecretInput{
		SecretId:                   aws.String(key),
		ForceDeleteWithoutRecovery: aws.Bool(true),
	})
	return err
}

func (s *AWSSecretsManager) ListSecrets(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	var nextToken *string
	for {
		output, err := s.client.ListSecrets(ctx, &secretsmanager.ListSecretsInput{
			NextToken: nextToken,
		})
		if err != nil {
			return nil, err
		}
		for _, secret := range output.SecretList {
			name := aws.ToString(secret.Name)
			if prefix == "" || strings.HasPrefix(name, prefix) {
				keys = append(keys, name)
			}
		}
		nextToken = output.NextToken
		if nextToken == nil {
			break
		}
	}
	return keys, nil
}

func isAWSNotFound(err error) bool {
	return strings.Contains(err.Error(), "ResourceNotFoundException") ||
		strings.Contains(err.Error(), "not found")
}
