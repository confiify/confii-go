// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build gcp

package cloud

import (
	"context"
	"fmt"
	"strings"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	confii "github.com/confiify/confii-go/v2"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GCPSecretManager implements SecretStore for GCP Secret Manager.
type GCPSecretManager struct {
	client    *secretmanager.Client
	projectID string
}

// Close releases the underlying Secret Manager client connections.
func (s *GCPSecretManager) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

// GCPSecretManagerOption configures GCPSecretManager.
type GCPSecretManagerOption func(*gcpSMConfig)

type gcpSMConfig struct {
	CredentialsFile string
}

// WithGCPCredentialsFile sets a service-account JSON file. When omitted, the
// Google Application Default Credentials chain is used.
func WithGCPCredentialsFile(path string) GCPSecretManagerOption {
	return func(c *gcpSMConfig) { c.CredentialsFile = path }
}

// NewGCPSecretManager creates a client scoped to projectID. ctx controls client
// initialization. Call Close when the store is no longer needed.
func NewGCPSecretManager(ctx context.Context, projectID string, opts ...GCPSecretManagerOption) (*GCPSecretManager, error) {
	cfg := &gcpSMConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	var clientOpts []option.ClientOption
	if cfg.CredentialsFile != "" {
		// Google deprecated WithCredentialsFile over the risk of long-lived
		// service-account key files. Confii does not create that risk; it honors
		// a path the deployment chose to configure, and dropping support would
		// break every deployment that relies on it. Migrating to workload
		// identity is the deployment's decision, not this library's, so the call
		// stays and the warning is suppressed deliberately rather than by a
		// blanket linter exclusion.
		//nolint:staticcheck // SA1019: see above; removal is a breaking change.
		clientOpts = append(clientOpts, option.WithCredentialsFile(cfg.CredentialsFile))
	}

	client, err := secretmanager.NewClient(ctx, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("gcp secret manager client: %w", err)
	}

	return &GCPSecretManager{client: client, projectID: projectID}, nil
}

// GetSecret returns a UTF-8 string for the requested version, defaulting to
// "latest". Field selection is not supported. Provider failures wrap
// [confii.ErrSecretAccess].
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
		return nil, fmt.Errorf("%w: %w", confii.ErrSecretAccess, err)
	}

	return string(resp.Payload.Data), nil
}

// SetSecret creates key with automatic replication when absent, then appends a
// version containing fmt.Sprint(value). Secret options are ignored.
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
	// Match the typed gRPC status rather than message text so only the
	// genuine already-exists condition is tolerated.
	if err != nil && !isGCPAlreadyExists(err) {
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

// isGCPAlreadyExists reports the typed gRPC condition returned when a secret
// already exists. Message text is deliberately ignored so unrelated provider
// failures cannot be treated as successful create-or-update admission.
func isGCPAlreadyExists(err error) bool {
	return status.Code(err) == codes.AlreadyExists
}

// DeleteSecret permanently deletes key and all versions. Secret options are ignored.
func (s *GCPSecretManager) DeleteSecret(ctx context.Context, key string, _ ...confii.SecretOption) error {
	name := fmt.Sprintf("projects/%s/secrets/%s", s.projectID, key)
	return s.client.DeleteSecret(ctx, &secretmanagerpb.DeleteSecretRequest{
		Name: name,
	})
}

// ListSecrets returns project secret names beginning with prefix, following all
// pages. Ordering is provider-defined.
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
