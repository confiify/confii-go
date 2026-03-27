//go:build gcp

package cloud

import (
	"context"
	"fmt"
	"strings"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	confii "github.com/qualitycoe/confii-go"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// GCPSecretManager implements SecretStore for GCP Secret Manager.
type GCPSecretManager struct {
	client    *secretmanager.Client
	projectID string
}

// GCPSecretManagerOption configures GCPSecretManager.
type GCPSecretManagerOption func(*gcpSMConfig)

type gcpSMConfig struct {
	CredentialsFile string
}

// WithGCPCredentialsFile sets the path to a service account key file.
func WithGCPCredentialsFile(path string) GCPSecretManagerOption {
	return func(c *gcpSMConfig) { c.CredentialsFile = path }
}

// NewGCPSecretManager creates a new GCP Secret Manager store.
func NewGCPSecretManager(ctx context.Context, projectID string, opts ...GCPSecretManagerOption) (*GCPSecretManager, error) {
	cfg := &gcpSMConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	var clientOpts []option.ClientOption
	if cfg.CredentialsFile != "" {
		clientOpts = append(clientOpts, option.WithCredentialsFile(cfg.CredentialsFile))
	}

	client, err := secretmanager.NewClient(ctx, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("gcp secret manager client: %w", err)
	}

	return &GCPSecretManager{client: client, projectID: projectID}, nil
}

func (s *GCPSecretManager) GetSecret(ctx context.Context, key string, opts ...confii.SecretOption) (any, error) {
	o := confii.ResolveSecretOptions(opts...)
	version := o.Version
	if version == "" {
		version = "latest"
	}

	name := fmt.Sprintf("projects/%s/secrets/%s/versions/%s", s.projectID, key, version)
	resp, err := s.client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
		Name: name,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", confii.ErrSecretAccess, err)
	}

	return string(resp.Payload.Data), nil
}

func (s *GCPSecretManager) SetSecret(ctx context.Context, key string, value any, _ ...confii.SecretOption) error {
	parent := fmt.Sprintf("projects/%s", s.projectID)
	secretName := fmt.Sprintf("projects/%s/secrets/%s", s.projectID, key)

	// Try to create the secret first.
	_, err := s.client.CreateSecret(ctx, &secretmanagerpb.CreateSecretRequest{
		Parent:   parent,
		SecretId: key,
		Secret: &secretmanagerpb.Secret{
			Replication: &secretmanagerpb.Replication{
				Replication: &secretmanagerpb.Replication_Automatic_{
					Automatic: &secretmanagerpb.Replication_Automatic{},
				},
			},
		},
	})
	if err != nil && !strings.Contains(err.Error(), "AlreadyExists") {
		return err
	}

	// Add the version.
	_, err = s.client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{
		Parent: secretName,
		Payload: &secretmanagerpb.SecretPayload{
			Data: []byte(fmt.Sprintf("%v", value)),
		},
	})
	return err
}

func (s *GCPSecretManager) DeleteSecret(ctx context.Context, key string, _ ...confii.SecretOption) error {
	name := fmt.Sprintf("projects/%s/secrets/%s", s.projectID, key)
	return s.client.DeleteSecret(ctx, &secretmanagerpb.DeleteSecretRequest{
		Name: name,
	})
}

func (s *GCPSecretManager) ListSecrets(ctx context.Context, prefix string) ([]string, error) {
	parent := fmt.Sprintf("projects/%s", s.projectID)
	it := s.client.ListSecrets(ctx, &secretmanagerpb.ListSecretsRequest{
		Parent: parent,
	})

	var keys []string
	for {
		secret, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		// Extract name from full resource path.
		parts := strings.Split(secret.Name, "/")
		name := parts[len(parts)-1]
		if prefix == "" || strings.HasPrefix(name, prefix) {
			keys = append(keys, name)
		}
	}
	return keys, nil
}
