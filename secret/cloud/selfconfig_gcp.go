//go:build gcp

package cloud

import (
	"context"
	"fmt"

	confii "github.com/confiify/confii-go"
)

func init() {
	confii.RegisterSelfConfigSecretProvider("gcp", func(cfg map[string]any) (confii.SelfConfigSecretStore, error) {
		projectID := selfString(cfg, "project_id", "project")
		if projectID == "" {
			return nil, fmt.Errorf("gcp provider requires project_id")
		}
		var opts []GCPSecretManagerOption
		if path := selfString(cfg, "credentials_file", "credentials_path"); path != "" {
			opts = append(opts, WithGCPCredentialsFile(path))
		}
		store, err := NewGCPSecretManager(context.Background(), projectID, opts...)
		if err != nil {
			return nil, err
		}
		return selfConfigStoreAdapter{get: func(ctx context.Context, key string) (any, error) {
			return store.GetSecret(ctx, key)
		}}, nil
	})
}
