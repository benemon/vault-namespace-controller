package vault

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/benemon/vault-namespace-controller/pkg/config"
	"github.com/hashicorp/vault/api"
	auth "github.com/hashicorp/vault/api/auth/kubernetes"
)

// Client provides methods for interacting with Vault Enterprise namespaces
type Client interface {
	// NamespaceExists checks if a namespace exists
	NamespaceExists(ctx context.Context, path string) (bool, error)

	// CreateNamespace creates a namespace
	CreateNamespace(ctx context.Context, path string) error

	// DeleteNamespace deletes a namespace
	DeleteNamespace(ctx context.Context, path string) error
}

// vaultClient implements the Client interface
type vaultClient struct {
	client *api.Client
	config *config.VaultConfig
}

// NewClient creates a new Vault client
func NewClient(config config.VaultConfig) (Client, error) {
	// Create Vault API client configuration
	clientConfig := api.DefaultConfig()
	clientConfig.Address = config.Address

	// Configure TLS if specified
	if config.CACert != "" || config.ClientCert != "" || config.ClientKey != "" || config.Insecure {
		tlsConfig := &api.TLSConfig{
			CACert:     config.CACert,
			ClientCert: config.ClientCert,
			ClientKey:  config.ClientKey,
			Insecure:   config.Insecure,
		}
		if err := clientConfig.ConfigureTLS(tlsConfig); err != nil {
			return nil, fmt.Errorf("failed to configure TLS: %w", err)
		}
	}

	// Create the client
	client, err := api.NewClient(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Vault client: %w", err)
	}

	// Set namespace root if specified
	if config.NamespaceRoot != "" {
		// Ensure namespace root is valid (remove leading/trailing slashes)
		nsRoot := strings.Trim(config.NamespaceRoot, "/")
		if nsRoot != "" {
			client.SetNamespace(nsRoot)
		}
	}

	// Authenticate based on the configured method
	if err := authenticate(client, config); err != nil {
		return nil, fmt.Errorf("failed to authenticate to Vault: %w", err)
	}

	return &vaultClient{
		client: client,
		config: &config,
	}, nil
}

// authenticate authenticates to Vault using the configured method
func authenticate(client *api.Client, config config.VaultConfig) error {
	// Store the current namespace to restore it later
	currentNamespace := client.Namespace()

	// If auth has a different namespace, set it temporarily
	if config.Auth.Namespace != "" {
		client.SetNamespace(strings.Trim(config.Auth.Namespace, "/"))
		defer client.SetNamespace(currentNamespace) // Restore original namespace after auth
	}

	switch config.Auth.Type {
	case "token":
		// Get token either directly or from file
		token := config.Auth.Token
		if token == "" && config.Auth.TokenPath != "" {
			// Read token from file
			tokenBytes, err := os.ReadFile(config.Auth.TokenPath)
			if err != nil {
				return fmt.Errorf("failed to read token from file: %w", err)
			}
			token = strings.TrimSpace(string(tokenBytes))
		}

		client.SetToken(token)
		return nil

	case "kubernetes":
		// Determine the auth path
		kubernetesAuthPath := "kubernetes"
		if config.Auth.Path != "" {
			kubernetesAuthPath = config.Auth.Path
		}

		k8sAuth, err := auth.NewKubernetesAuth(
			config.Auth.Role,
			auth.WithServiceAccountTokenPath("/var/run/secrets/kubernetes.io/serviceaccount/token"),
			auth.WithMountPath(kubernetesAuthPath),
		)
		if err != nil {
			return fmt.Errorf("failed to initialize Kubernetes auth: %w", err)
		}

		authInfo, err := client.Auth().Login(context.Background(), k8sAuth)
		if err != nil {
			return fmt.Errorf("failed to login with Kubernetes auth: %w", err)
		}

		if authInfo == nil {
			return fmt.Errorf("no auth info was returned after login")
		}

		return nil

	case "approle":
		// Determine the AppRole auth path
		appRoleAuthPath := "approle"
		if config.Auth.Path != "" {
			appRoleAuthPath = config.Auth.Path
		}

		// Get roleID and secretID either directly or from files
		roleID := config.Auth.RoleID
		secretID := config.Auth.SecretID

		if roleID == "" && config.Auth.RoleIDPath != "" {
			// Read roleID from file
			roleIDBytes, err := os.ReadFile(config.Auth.RoleIDPath)
			if err != nil {
				return fmt.Errorf("failed to read roleID from file: %w", err)
			}
			roleID = strings.TrimSpace(string(roleIDBytes))
		}

		if secretID == "" && config.Auth.SecretIDPath != "" {
			// Read secretID from file
			secretIDBytes, err := os.ReadFile(config.Auth.SecretIDPath)
			if err != nil {
				return fmt.Errorf("failed to read secretID from file: %w", err)
			}
			secretID = strings.TrimSpace(string(secretIDBytes))
		}

		data := map[string]interface{}{
			"role_id":   roleID,
			"secret_id": secretID,
		}

		loginPath := fmt.Sprintf("auth/%s/login", appRoleAuthPath)
		resp, err := client.Logical().Write(loginPath, data)
		if err != nil {
			return fmt.Errorf("failed to login with AppRole: %w", err)
		}

		if resp == nil || resp.Auth == nil {
			return fmt.Errorf("no auth info was returned after AppRole login")
		}

		client.SetToken(resp.Auth.ClientToken)
		return nil

	default:
		return fmt.Errorf("unsupported auth method: %s", config.Auth.Type)
	}
}

// NamespaceExists checks if a namespace exists in Vault
func (c *vaultClient) NamespaceExists(ctx context.Context, namespacePath string) (bool, error) {
	// Get parent namespace and child namespace
	parent, child := splitNamespacePath(namespacePath)

	// Save current namespace to restore later
	currentNamespace := c.client.Namespace()

	// Temporarily set namespace to parent (or root if empty)
	if parent != "" {
		c.client.SetNamespace(parent)
	} else {
		c.client.SetNamespace("")
	}
	// Make sure we restore the original namespace when we're done
	defer c.client.SetNamespace(currentNamespace)

	// Use Logical().List which handles namespaces correctly
	secret, err := c.client.Logical().ListWithContext(ctx, "sys/namespaces")
	if err != nil {
		// Check for 404 errors which might indicate parent doesn't exist
		if strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, fmt.Errorf("failed to list namespaces: %w", err)
	}

	// Handle nil response
	if secret == nil || secret.Data == nil {
		return false, nil
	}

	// Extract list of keys
	keys, ok := secret.Data["keys"].([]interface{})
	if !ok {
		return false, fmt.Errorf("unexpected response format: 'keys' is not a list")
	}

	// Look for the child namespace
	for _, key := range keys {
		keyStr, ok := key.(string)
		if !ok {
			continue
		}

		// Vault returns namespaces with trailing slashes
		if strings.TrimSuffix(keyStr, "/") == child {
			return true, nil
		}
	}

	return false, nil
}

// CreateNamespace creates a namespace in Vault Enterprise
func (c *vaultClient) CreateNamespace(ctx context.Context, namespacePath string) error {
	// Get parent namespace and child namespace
	parent, child := splitNamespacePath(namespacePath)

	// Set namespace to parent for this request
	headers := map[string][]string{
		"X-Vault-Namespace": {parent},
	}

	// Create the namespace
	req := c.client.NewRequest("POST", fmt.Sprintf("/v1/sys/namespaces/%s", child))
	req.Headers = headers

	resp, err := c.client.RawRequestWithContext(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to create namespace %s: %w", namespacePath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return fmt.Errorf("unexpected status code when creating namespace: %d", resp.StatusCode)
	}

	return nil
}

// DeleteNamespace deletes a namespace in Vault Enterprise
func (c *vaultClient) DeleteNamespace(ctx context.Context, namespacePath string) error {
	// Get parent namespace and child namespace
	parent, child := splitNamespacePath(namespacePath)

	// Set namespace to parent for this request
	headers := map[string][]string{
		"X-Vault-Namespace": {parent},
	}

	// Delete the namespace
	req := c.client.NewRequest("DELETE", fmt.Sprintf("/v1/sys/namespaces/%s", child))
	req.Headers = headers

	resp, err := c.client.RawRequestWithContext(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to delete namespace %s: %w", namespacePath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return fmt.Errorf("unexpected status code when deleting namespace: %d", resp.StatusCode)
	}

	return nil
}

// splitNamespacePath splits a namespace path into parent and child components
func splitNamespacePath(namespacePath string) (parent, child string) {
	// Clean the path by removing leading/trailing slashes
	cleanPath := strings.Trim(namespacePath, "/")

	// If there's no separator, return empty parent and the path as child
	if !strings.Contains(cleanPath, "/") {
		return "", cleanPath
	}

	// Split the path into parent and child
	dir, base := path.Split(cleanPath)

	// Clean the parent by removing trailing slash
	parent = strings.TrimSuffix(dir, "/")

	return parent, base
}
