//go:build azure

package cloud

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
	confii "github.com/qualitycoe/confii-go"
)

var azureSecretNameRegex = regexp.MustCompile(`^[0-9a-zA-Z-]+$`)

// AzureKeyVault implements SecretStore for Azure Key Vault.
type AzureKeyVault struct {
	client *azsecrets.Client
}

// NewAzureKeyVault creates a new Azure Key Vault store.
// If credential is nil, DefaultAzureCredential is used.
func NewAzureKeyVault(vaultURL string, credential any) (*AzureKeyVault, error) {
	var client *azsecrets.Client
	var err error

	if credential != nil {
		if cred, ok := credential.(*azidentity.DefaultAzureCredential); ok {
			client, err = azsecrets.NewClient(vaultURL, cred, nil)
		} else {
			return nil, fmt.Errorf("unsupported credential type: %T", credential)
		}
	} else {
		cred, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("azure default credential: %w", err)
		}
		client, err = azsecrets.NewClient(vaultURL, cred, nil)
		if err != nil {
			return nil, fmt.Errorf("azure keyvault client: %w", err)
		}
	}

	if err != nil {
		return nil, err
	}
	return &AzureKeyVault{client: client}, nil
}

func (s *AzureKeyVault) GetSecret(ctx context.Context, key string, opts ...confii.SecretOption) (any, error) {
	if !azureSecretNameRegex.MatchString(key) {
		return nil, fmt.Errorf("%w: invalid secret name %q (must match ^[0-9a-zA-Z-]+$)", confii.ErrSecretValidation, key)
	}

	o := confii.ResolveSecretOptions(opts...)
	version := o.Version

	resp, err := s.client.GetSecret(ctx, key, version, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", confii.ErrSecretAccess, err)
	}

	if resp.Value == nil {
		return nil, fmt.Errorf("%w: %s", confii.ErrSecretNotFound, key)
	}
	return *resp.Value, nil
}

func (s *AzureKeyVault) SetSecret(ctx context.Context, key string, value any, _ ...confii.SecretOption) error {
	secretVal := fmt.Sprintf("%v", value)
	_, err := s.client.SetSecret(ctx, key, azsecrets.SetSecretParameters{
		Value: &secretVal,
	}, nil)
	return err
}

func (s *AzureKeyVault) DeleteSecret(ctx context.Context, key string, _ ...confii.SecretOption) error {
	_, err := s.client.DeleteSecret(ctx, key, nil)
	return err
}

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
