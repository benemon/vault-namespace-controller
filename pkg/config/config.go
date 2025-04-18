package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

// Common errors
var (
	ErrMissingVaultAddress = errors.New("vault address is required")
	ErrMissingAuthType     = errors.New("vault auth type is required")
	ErrUnsupportedAuthType = errors.New("unsupported auth method")
)

// VaultAuthConfig contains configuration for Vault authentication.
type VaultAuthConfig struct {
	// Type specifies the auth method: kubernetes, token, or approle.
	Type string `yaml:"type"`

	// Path specifies the custom path where the auth method is mounted.
	Path string `yaml:"path,omitempty"`

	// Namespace specifies the namespace where the auth method resides.
	Namespace string `yaml:"namespace,omitempty"`

	// Token auth
	Token     string `yaml:"token,omitempty"`
	TokenPath string `yaml:"tokenPath,omitempty"`

	// Kubernetes auth
	Role string `yaml:"role,omitempty"`

	// AppRole auth
	RoleID       string `yaml:"roleId,omitempty"`
	SecretID     string `yaml:"secretId,omitempty"`
	RoleIDPath   string `yaml:"roleIdPath,omitempty"`
	SecretIDPath string `yaml:"secretIdPath,omitempty"`
}

// VaultConfig contains configuration for connecting to Vault.
type VaultConfig struct {
	// Address specifies the Vault server address.
	Address string `yaml:"address"`

	// NamespaceRoot specifies the root namespace path in Vault.
	NamespaceRoot string `yaml:"namespaceRoot,omitempty"`

	// Auth contains authentication configuration.
	Auth VaultAuthConfig `yaml:"auth"`

	// TLS config
	CACert     string `yaml:"caCert,omitempty"`
	ClientCert string `yaml:"clientCert,omitempty"`
	ClientKey  string `yaml:"clientKey,omitempty"`
	Insecure   bool   `yaml:"insecure,omitempty"`
}

// ControllerConfig contains all configuration for the controller.
type ControllerConfig struct {
	// Vault configuration
	Vault VaultConfig `yaml:"vault"`

	// ReconcileInterval specifies how often to reconcile namespaces (in seconds).
	ReconcileInterval int `yaml:"reconcileInterval"`

	// DeleteVaultNamespaces indicates whether to delete Vault namespaces when
	// the corresponding Kubernetes namespace is deleted.
	DeleteVaultNamespaces bool `yaml:"deleteVaultNamespaces"` // Removed omitempty to ensure it's always included in YAML

	// NamespaceFormat specifies the format string for Vault namespace names.
	NamespaceFormat string `yaml:"namespaceFormat"`

	// IncludeNamespaces specifies patterns of namespaces to include.
	IncludeNamespaces []string `yaml:"includeNamespaces,omitempty"`

	// ExcludeNamespaces specifies patterns of namespaces to exclude.
	ExcludeNamespaces []string `yaml:"excludeNamespaces,omitempty"`

	// MetricsBindAddress specifies the address to bind metrics server.
	MetricsBindAddress string `yaml:"metricsBindAddress"`

	// LeaderElection indicates whether to use leader election.
	LeaderElection bool `yaml:"leaderElection"` // Removed omitempty to ensure it's always included in YAML
}

// LoadConfig loads configuration from a file. If path is empty, default configuration is returned.
func LoadConfig(path string) (*ControllerConfig, error) {
	config := &ControllerConfig{
		// Default values
		ReconcileInterval:     300, // 5 minutes
		DeleteVaultNamespaces: true,
		MetricsBindAddress:    ":8080",
		LeaderElection:        true,
		NamespaceFormat:       "%s", // default format is the namespace name
	}

	// If path is empty, return default config
	if path == "" {
		return config, nil
	}

	// Read config file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
	}

	// Parse config - use a temporary struct to ensure all fields are properly unmarshaled
	var tempConfig ControllerConfig
	if err := yaml.Unmarshal(data, &tempConfig); err != nil {
		return nil, fmt.Errorf("failed to parse config file %q: %w", path, err)
	}

	// Now manually copy the values from tempConfig to config
	// This ensures that values not present in the YAML don't keep their defaults

	// Vault config is different, only copy if it's set
	if tempConfig.Vault.Address != "" {
		config.Vault = tempConfig.Vault
	}

	// Copy direct fields, checking if they exist in the YAML
	if tempConfig.ReconcileInterval != 0 {
		config.ReconcileInterval = tempConfig.ReconcileInterval
	}

	// For boolean fields, we need to use the value from tempConfig
	// DeleteVaultNamespaces and LeaderElection need to be overridden regardless
	config.DeleteVaultNamespaces = tempConfig.DeleteVaultNamespaces
	config.LeaderElection = tempConfig.LeaderElection

	// String fields, check if non-empty
	if tempConfig.NamespaceFormat != "" {
		config.NamespaceFormat = tempConfig.NamespaceFormat
	}
	if tempConfig.MetricsBindAddress != "" {
		config.MetricsBindAddress = tempConfig.MetricsBindAddress
	}

	// Slice fields, check if non-nil
	if tempConfig.IncludeNamespaces != nil {
		config.IncludeNamespaces = tempConfig.IncludeNamespaces
	}
	if tempConfig.ExcludeNamespaces != nil {
		config.ExcludeNamespaces = tempConfig.ExcludeNamespaces
	}

	// Validate config
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}

// validateConfig checks that the configuration is valid.
func validateConfig(config *ControllerConfig) error {
	// Validate Vault address
	if config.Vault.Address == "" {
		return ErrMissingVaultAddress
	}

	// Validate auth configuration
	if config.Vault.Auth.Type == "" {
		return ErrMissingAuthType
	}

	// Validate auth method
	switch config.Vault.Auth.Type {
	case "token":
		if config.Vault.Auth.Token == "" && config.Vault.Auth.TokenPath == "" {
			return errors.New("either token or tokenPath is required for token auth method")
		}
	case "kubernetes":
		if config.Vault.Auth.Role == "" {
			return errors.New("role is required for kubernetes auth method")
		}
	case "approle":
		// Check direct values
		hasDirectValues := config.Vault.Auth.RoleID != "" && config.Vault.Auth.SecretID != ""
		// Check path values
		hasPathValues := config.Vault.Auth.RoleIDPath != "" && config.Vault.Auth.SecretIDPath != ""

		if !hasDirectValues && !hasPathValues {
			return errors.New("either roleId+secretId or roleIdPath+secretIdPath are required for approle auth method")
		}
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedAuthType, config.Vault.Auth.Type)
	}

	return nil
}
