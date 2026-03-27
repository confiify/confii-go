//go:build aws

package cloud

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	confii "github.com/qualitycoe/confii-go"
	"github.com/qualitycoe/confii-go/internal/dictutil"
	"github.com/qualitycoe/confii-go/internal/typecoerce"
)

// SSMLoader loads configuration from AWS Systems Manager Parameter Store.
type SSMLoader struct {
	pathPrefix string
	decrypt    bool
	region     string
	accessKey  string
	secretKey  string
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

// NewSSM creates a new SSM Parameter Store loader.
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

func (l *SSMLoader) Source() string { return "ssm:" + l.pathPrefix }

func (l *SSMLoader) Load(ctx context.Context) (map[string]any, error) {
	cfg, err := l.awsConfig(ctx)
	if err != nil {
		return nil, confii.NewLoadError(l.Source(), err)
	}

	client := ssm.NewFromConfig(cfg)
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
			value := typecoerce.ParseScalar(aws.ToString(param.Value), false)
			_ = dictutil.SetNested(result, keyPath, value)
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
