// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build azure

package cloud

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
	confii "github.com/confiify/confii-go/v2"
)

var azureSecretNameRegex = regexp.MustCompile(`^[0-9a-zA-Z-]+$`)

// AzureKeyVault implements SecretStore for Azure Key Vault.
type AzureKeyVault struct {
	client *azsecrets.Client
}

// NewAzureKeyVault creates a new Azure Key Vault store backed by [azsecrets.NewClient].
//
// The credential parameter accepts ANY implementation of [azcore.TokenCredential]
// — the Azure SDK's documented credential interface. This includes, but is not
// limited to:
//
//   - [*azidentity.DefaultAzureCredential]
//   - [*azidentity.ClientSecretCredential]
//   - [*azidentity.ManagedIdentityCredential]
//   - [*azidentity.ClientCertificateCredential]
//   - [*azidentity.WorkloadIdentityCredential]
//   - any custom [azcore.TokenCredential] implementation (test fakes, broker
//     integrations, etc.)
//
// If credential is nil, a [*azidentity.DefaultAzureCredential] is constructed
// implicitly.
//
// The constructor accepts any typed [azcore.TokenCredential], avoiding runtime
// type assertions and supporting custom credentials and test fakes.
func NewAzureKeyVault(vaultURL string, credential azcore.TokenCredential) (*AzureKeyVault, error) {
	if credential == nil {
		def, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("azure default credential: %w", err)
		}
		credential = def
	}

	client, err := azsecrets.NewClient(vaultURL, credential, nil)
	if err != nil {
		return nil, fmt.Errorf("azure keyvault client: %w", err)
	}
	return &AzureKeyVault{client: client}, nil
}

// GetSecret accepts Azure names matching ^[0-9a-zA-Z-]+$ and optionally selects
// a version with [confii.WithVersion]. Invalid names wrap
// [confii.ErrSecretValidation], absent values wrap [confii.ErrSecretNotFound],
// and provider failures wrap [confii.ErrSecretAccess]. Field selection is not
// supported.
func (s *AzureKeyVault) GetSecret(ctx context.Context, key string, opts ...confii.SecretOption) (any, error) {
	if !azureSecretNameRegex.MatchString(key) {
		return nil, fmt.Errorf("%w: invalid secret name %q (must match ^[0-9a-zA-Z-]+$)", confii.ErrSecretValidation, key)
	}

	o := confii.ResolveSecretOptions(opts...)
	version := o.Version

	resp, err := s.client.GetSecret(ctx, key, version, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", confii.ErrSecretAccess, err)
	}

	if resp.Value == nil {
		return nil, fmt.Errorf("%w: %s", confii.ErrSecretNotFound, key)
	}
	return *resp.Value, nil
}

// SetSecret creates or updates key with fmt.Sprint(value). Secret options are ignored.
func (s *AzureKeyVault) SetSecret(ctx context.Context, key string, value any, _ ...confii.SecretOption) error {
	secretVal := fmt.Sprintf("%v", value)
	_, err := s.client.SetSecret(ctx, key, azsecrets.SetSecretParameters{
		Value: &secretVal,
	}, nil)
	return err
}

// DeleteSecret starts Azure Key Vault's asynchronous delete operation. Recovery
// and purge behavior follow the vault's retention policy; this method does not
// wait for purge. Secret options are ignored.
func (s *AzureKeyVault) DeleteSecret(ctx context.Context, key string, _ ...confii.SecretOption) error {
	_, err := s.client.DeleteSecret(ctx, key, nil)
	return err
}

// ListSecrets returns names beginning with prefix while following all pages.
// Ordering is provider-defined.
func (s *AzureKeyVault) ListSecrets(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	pager := s.client.NewListSecretPropertiesPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Value {
			name := item.ID.Name()
			if prefix == "" || strings.HasPrefix(name, prefix) {
				keys = append(keys, name)
			}
		}
	}
	return keys, nil
}
