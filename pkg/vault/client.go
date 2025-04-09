package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/benemon/vault-namespace-controller/pkg/config"
	"github.com/benemon/vault-namespace-controller/pkg/metrics"
	"github.com/hashicorp/vault/api"
	auth "github.com/hashicorp/vault/api/auth/kubernetes"
)

// Common error definitions
var (
	ErrVaultClientCreate       = errors.New("failed to create vault client")
	ErrVaultTLSConfig          = errors.New("failed to configure TLS for vault client")
	ErrVaultAuth               = errors.New("failed to authenticate to vault")
	ErrVaultNamespaceOperation = errors.New("vault namespace operation failed")
	ErrVaultNamespaceNotFound  = errors.New("vault namespace not found")
)

// Client provides methods for interacting with Vault Enterprise namespaces.
type Client interface {
	NamespaceExists(ctx context.Context, path string) (bool, error)
	CreateNamespace(ctx context.Context, path string) error
	DeleteNamespace(ctx context.Context, path string) error
}

type vaultClient struct {
	client *api.Client
	config *config.VaultConfig
}

func splitNamespacePath(namespacePath string) (parent, child string) {
	cleanPath := strings.Trim(namespacePath, "/")
	if !strings.Contains(cleanPath, "/") {
		return "", cleanPath
	}
	dir, base := path.Split(cleanPath)
	parent = strings.TrimSuffix(dir, "/")
	return parent, base
}

func NewClient(config config.VaultConfig) (Client, error) {
	clientConfig := api.DefaultConfig()
	clientConfig.Address = config.Address

	if config.CACert != "" || config.ClientCert != "" || config.ClientKey != "" || config.Insecure {
		tlsConfig := &api.TLSConfig{
			CACert:     config.CACert,
			ClientCert: config.ClientCert,
			ClientKey:  config.ClientKey,
			Insecure:   config.Insecure,
		}
		if err := clientConfig.ConfigureTLS(tlsConfig); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrVaultTLSConfig, err)
		}
	}

	client, err := api.NewClient(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVaultClientCreate, err)
	}

	if config.NamespaceRoot != "" {
		nsRoot := strings.Trim(config.NamespaceRoot, "/")
		if nsRoot != "" {
			client.SetNamespace(nsRoot)
		}
	}

	if err := authenticate(client, config); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVaultAuth, err)
	}

	return &vaultClient{
		client: client,
		config: &config,
	}, nil
}

func authenticate(client *api.Client, config config.VaultConfig) error {
	authType := config.Auth.Type
	metrics.VaultAuthOperationsTotal.WithLabelValues(authType).Inc()
	start := time.Now()

	currentNamespace := client.Namespace()
	if config.Auth.Namespace != "" {
		client.SetNamespace(strings.Trim(config.Auth.Namespace, "/"))
		defer client.SetNamespace(currentNamespace)
	}

	var err error
	switch authType {
	case "token":
		err = authenticateWithToken(client, config)
	case "kubernetes":
		err = authenticateWithKubernetes(client, config)
	case "approle":
		err = authenticateWithAppRole(client, config)
	default:
		err = fmt.Errorf("unsupported auth method: %s", authType)
	}

	duration := time.Since(start).Seconds()
	metrics.VaultAuthDuration.WithLabelValues(authType).Observe(duration)

	if err != nil {
		metrics.VaultAuthErrorsTotal.WithLabelValues(authType).Inc()
	}

	return err
}

func authenticateWithToken(client *api.Client, config config.VaultConfig) error {
	token := config.Auth.Token
	if token == "" && config.Auth.TokenPath != "" {
		tokenBytes, err := os.ReadFile(config.Auth.TokenPath)
		if err != nil {
			return fmt.Errorf("failed to read token from file %q: %w", config.Auth.TokenPath, err)
		}
		token = strings.TrimSpace(string(tokenBytes))
	}
	client.SetToken(token)
	return nil
}

func authenticateWithKubernetes(client *api.Client, config config.VaultConfig) error {
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
		return fmt.Errorf("failed to initialize kubernetes auth: %w", err)
	}
	authInfo, err := client.Auth().Login(context.Background(), k8sAuth)
	if err != nil {
		return fmt.Errorf("failed to login with kubernetes auth: %w", err)
	}
	if authInfo == nil {
		return errors.New("no auth info was returned after login")
	}
	return nil
}

func authenticateWithAppRole(client *api.Client, config config.VaultConfig) error {
	appRoleAuthPath := "approle"
	if config.Auth.Path != "" {
		appRoleAuthPath = config.Auth.Path
	}

	roleID := config.Auth.RoleID
	secretID := config.Auth.SecretID

	if roleID == "" && config.Auth.RoleIDPath != "" {
		roleIDBytes, err := os.ReadFile(config.Auth.RoleIDPath)
		if err != nil {
			return fmt.Errorf("failed to read roleID from file %q: %w", config.Auth.RoleIDPath, err)
		}
		roleID = strings.TrimSpace(string(roleIDBytes))
	}
	if secretID == "" && config.Auth.SecretIDPath != "" {
		secretIDBytes, err := os.ReadFile(config.Auth.SecretIDPath)
		if err != nil {
			return fmt.Errorf("failed to read secretID from file %q: %w", config.Auth.SecretIDPath, err)
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
		return fmt.Errorf("failed to login with approle: %w", err)
	}
	if resp == nil || resp.Auth == nil {
		return errors.New("no auth info was returned after approle login")
	}
	client.SetToken(resp.Auth.ClientToken)
	return nil
}

func (c *vaultClient) NamespaceExists(ctx context.Context, namespacePath string) (bool, error) {
	start := time.Now()
	metrics.VaultOperationsTotal.WithLabelValues("check", "attempt").Inc()

	parent, child := splitNamespacePath(namespacePath)
	currentNamespace := c.client.Namespace()
	if parent != "" {
		c.client.SetNamespace(parent)
	} else {
		c.client.SetNamespace("")
	}
	defer c.client.SetNamespace(currentNamespace)

	secret, err := c.client.Logical().ListWithContext(ctx, "sys/namespaces")
	duration := time.Since(start).Seconds()
	metrics.VaultOperationDuration.WithLabelValues("check").Observe(duration)

	if err != nil {
		metrics.VaultOperationsTotal.WithLabelValues("check", "error").Inc()
		if strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, fmt.Errorf("failed to list namespaces in %q: %w", parent, err)
	}

	if secret == nil || secret.Data == nil {
		metrics.VaultOperationsTotal.WithLabelValues("check", "not_found").Inc()
		return false, nil
	}

	keys, ok := secret.Data["keys"].([]interface{})
	if !ok {
		metrics.VaultOperationsTotal.WithLabelValues("check", "error").Inc()
		return false, errors.New("unexpected response format when listing namespaces: 'keys' is not a list")
	}

	for _, key := range keys {
		keyStr, ok := key.(string)
		if !ok {
			continue
		}
		if strings.TrimSuffix(keyStr, "/") == child {
			metrics.VaultOperationsTotal.WithLabelValues("check", "success").Inc()
			return true, nil
		}
	}
	metrics.VaultOperationsTotal.WithLabelValues("check", "not_found").Inc()
	return false, nil
}

func (c *vaultClient) CreateNamespace(ctx context.Context, namespacePath string) error {
	start := time.Now()
	metrics.VaultOperationsTotal.WithLabelValues("create", "attempt").Inc()

	parent, child := splitNamespacePath(namespacePath)
	headers := map[string][]string{
		"X-Vault-Namespace": {parent},
	}

	req := c.client.NewRequest("POST", fmt.Sprintf("/v1/sys/namespaces/%s", child))
	req.Headers = headers

	resp, err := c.client.RawRequestWithContext(ctx, req)
	duration := time.Since(start).Seconds()
	metrics.VaultOperationDuration.WithLabelValues("create").Observe(duration)

	if err != nil {
		metrics.VaultOperationsTotal.WithLabelValues("create", "error").Inc()
		return fmt.Errorf("%w: failed to create namespace %q: %v", ErrVaultNamespaceOperation, namespacePath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		metrics.VaultOperationsTotal.WithLabelValues("create", "error").Inc()
		return fmt.Errorf("%w: unexpected status code when creating namespace %q: %d",
			ErrVaultNamespaceOperation, namespacePath, resp.StatusCode)
	}

	metrics.VaultOperationsTotal.WithLabelValues("create", "success").Inc()
	return nil
}

func (c *vaultClient) DeleteNamespace(ctx context.Context, namespacePath string) error {
	start := time.Now()
	metrics.VaultOperationsTotal.WithLabelValues("delete", "attempt").Inc()

	parent, child := splitNamespacePath(namespacePath)
	headers := map[string][]string{
		"X-Vault-Namespace": {parent},
	}

	req := c.client.NewRequest("DELETE", fmt.Sprintf("/v1/sys/namespaces/%s", child))
	req.Headers = headers

	resp, err := c.client.RawRequestWithContext(ctx, req)
	duration := time.Since(start).Seconds()
	metrics.VaultOperationDuration.WithLabelValues("delete").Observe(duration)

	if err != nil {
		metrics.VaultOperationsTotal.WithLabelValues("delete", "error").Inc()
		return fmt.Errorf("%w: failed to delete namespace %q: %v", ErrVaultNamespaceOperation, namespacePath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		metrics.VaultOperationsTotal.WithLabelValues("delete", "error").Inc()
		return fmt.Errorf("%w: unexpected status code when deleting namespace %q: %d",
			ErrVaultNamespaceOperation, namespacePath, resp.StatusCode)
	}

	metrics.VaultOperationsTotal.WithLabelValues("delete", "success").Inc()
	return nil
}

func (c *vaultClient) GetTokenTTL() (int64, error) {
	if c.config.Auth.Type != "token" && c.client.Token() == "" {
		return 0, nil
	}
	tokenInfo, err := c.client.Auth().Token().LookupSelf()
	if err != nil {
		return 0, fmt.Errorf("failed to lookup token: %w", err)
	}
	ttlRaw, ok := tokenInfo.Data["ttl"]
	if !ok {
		return 0, fmt.Errorf("TTL not found in token info")
	}

	var ttl int64
	switch v := ttlRaw.(type) {
	case json.Number:
		ttl, err = v.Int64()
		if err != nil {
			return 0, fmt.Errorf("failed to parse TTL as int64: %w", err)
		}
	case float64:
		ttl = int64(v)
	case int64:
		ttl = v
	case int:
		ttl = int64(v)
	default:
		return 0, fmt.Errorf("unexpected TTL type: %T", ttlRaw)
	}
	return ttl, nil
}
