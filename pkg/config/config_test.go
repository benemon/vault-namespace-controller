package config

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
)

func TestLoadConfig_Default(t *testing.T) {
	// Test loading default config when no file is provided
	config, err := LoadConfig("")

	assert.NoError(t, err)
	assert.NotNil(t, config)

	// Check default values
	assert.Equal(t, 300, config.ReconcileInterval)
	assert.True(t, config.DeleteVaultNamespaces)
	assert.Equal(t, ":8080", config.MetricsBindAddress)
	assert.True(t, config.LeaderElection)
	assert.Equal(t, "%s", config.NamespaceFormat)
}

func TestLoadConfig_FromFile(t *testing.T) {
	// Create a temporary config file
	configData := &ControllerConfig{
		Vault: VaultConfig{
			Address:       "https://vault.example.org:8200",
			NamespaceRoot: "/admin",
			Auth: VaultAuthConfig{
				Type:  "token",
				Token: "test-token",
			},
		},
		ReconcileInterval:     60,
		DeleteVaultNamespaces: false,
		NamespaceFormat:       "env-%s",
		IncludeNamespaces:     []string{"app-.*"},
		ExcludeNamespaces:     []string{"system-.*"},
		MetricsBindAddress:    ":9090",
		LeaderElection:        false,
	}

	// Convert to YAML
	data, err := yaml.Marshal(configData)
	assert.NoError(t, err)

	// Write to a temporary file
	tempFile, err := os.CreateTemp("", "config-*.yaml")
	assert.NoError(t, err)
	defer os.Remove(tempFile.Name())

	_, err = tempFile.Write(data)
	assert.NoError(t, err)
	err = tempFile.Close()
	assert.NoError(t, err)

	// Load the config from the file
	config, err := LoadConfig(tempFile.Name())

	assert.NoError(t, err)
	assert.NotNil(t, config)

	// Check values from the file
	assert.Equal(t, "https://vault.example.org:8200", config.Vault.Address)
	assert.Equal(t, "/admin", config.Vault.NamespaceRoot)
	assert.Equal(t, "token", config.Vault.Auth.Type)
	assert.Equal(t, "test-token", config.Vault.Auth.Token)
	assert.Equal(t, 60, config.ReconcileInterval)
	assert.Equal(t, false, config.DeleteVaultNamespaces)
	assert.Equal(t, "env-%s", config.NamespaceFormat)
	assert.Equal(t, []string{"app-.*"}, config.IncludeNamespaces)
	assert.Equal(t, []string{"system-.*"}, config.ExcludeNamespaces)
	assert.Equal(t, ":9090", config.MetricsBindAddress)
	assert.Equal(t, false, config.LeaderElection)
}

func TestLoadConfig_InvalidFile(t *testing.T) {
	// Create a temporary file with invalid YAML
	tempFile, err := os.CreateTemp("", "config-*.yaml")
	assert.NoError(t, err)
	defer os.Remove(tempFile.Name())

	_, err = tempFile.Write([]byte("invalid: yaml: content:"))
	assert.NoError(t, err)
	err = tempFile.Close()
	assert.NoError(t, err)

	// Try to load the config from the invalid file
	_, err = LoadConfig(tempFile.Name())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse config file")
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      *ControllerConfig
		expectedErr error
	}{
		{
			name: "valid token auth",
			config: &ControllerConfig{
				Vault: VaultConfig{
					Address: "https://vault.example.com:8200",
					Auth: VaultAuthConfig{
						Type:  "token",
						Token: "test-token",
					},
				},
			},
			expectedErr: nil,
		},
		{
			name: "valid kubernetes auth",
			config: &ControllerConfig{
				Vault: VaultConfig{
					Address: "https://vault.example.com:8200",
					Auth: VaultAuthConfig{
						Type: "kubernetes",
						Role: "vault-controller",
					},
				},
			},
			expectedErr: nil,
		},
		{
			name: "valid approle auth",
			config: &ControllerConfig{
				Vault: VaultConfig{
					Address: "https://vault.example.com:8200",
					Auth: VaultAuthConfig{
						Type:     "approle",
						RoleID:   "role-id",
						SecretID: "secret-id",
					},
				},
			},
			expectedErr: nil,
		},
		{
			name: "missing vault address",
			config: &ControllerConfig{
				Vault: VaultConfig{
					Auth: VaultAuthConfig{
						Type:  "token",
						Token: "test-token",
					},
				},
			},
			expectedErr: ErrMissingVaultAddress,
		},
		{
			name: "token auth without token",
			config: &ControllerConfig{
				Vault: VaultConfig{
					Address: "https://vault.example.com:8200",
					Auth: VaultAuthConfig{
						Type: "token",
					},
				},
			},
			expectedErr: errors.New("either token or tokenPath is required for token auth method"),
		},
		{
			name: "kubernetes auth without role",
			config: &ControllerConfig{
				Vault: VaultConfig{
					Address: "https://vault.example.com:8200",
					Auth: VaultAuthConfig{
						Type: "kubernetes",
					},
				},
			},
			expectedErr: errors.New("role is required for kubernetes auth method"),
		},
		{
			name: "approle auth without credentials",
			config: &ControllerConfig{
				Vault: VaultConfig{
					Address: "https://vault.example.com:8200",
					Auth: VaultAuthConfig{
						Type: "approle",
					},
				},
			},
			expectedErr: errors.New("either roleId+secretId or roleIdPath+secretIdPath are required for approle auth method"),
		},
		{
			name: "unsupported auth method",
			config: &ControllerConfig{
				Vault: VaultConfig{
					Address: "https://vault.example.com:8200",
					Auth: VaultAuthConfig{
						Type: "unsupported",
					},
				},
			},
			expectedErr: ErrUnsupportedAuthType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.config)

			if tt.expectedErr != nil {
				assert.Error(t, err)
				if errors.Is(err, tt.expectedErr) {
					// If we're expecting a specific error, check that it matches
					assert.True(t, errors.Is(err, tt.expectedErr))
				} else {
					// Otherwise, just check the error message
					assert.Contains(t, err.Error(), tt.expectedErr.Error())
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
