# Vault Namespace Controller - Installation Guide

This guide will walk you through installing and configuring the Vault Namespace Controller in your Kubernetes cluster using Helm.

## Prerequisites

- Kubernetes cluster (1.16+)
- Helm 3.0+
- Access to a Vault Enterprise instance with namespaces capability
- Appropriate permissions to create namespaces in Vault

## Installation Steps

### 1. Clone the repository or download the Helm chart

```bash
git clone https://github.com/benemon/vault-namespace-controller.git
cd vault-namespace-controller
```

### 2. Create a values file

Create a file named `values.yaml` with your specific configuration:

```yaml
# Basic configuration example
image:
  repository: ghcr.io/benemon/vault-namespace-controller
  tag: "0.1.0"

# Controller configuration
controller:
  reconcileInterval: 300
  deleteVaultNamespaces: true
  namespaceFormat: "k8s-%s"
  includeNamespaces:
    - "app-.*"
  excludeNamespaces:
    - "system-.*"

# Vault configuration
vault:
  address: "https://vault.example.com:8200"
  namespaceRoot: "/admin"  # For HCP Vault Dedicated
  
  # Authentication configuration
  auth:
    type: "kubernetes"
    role: "vault-namespace-controller"
```

### 3. Install the Helm chart

```bash
helm install vault-namespace-controller ./deploy/helm/vault-namespace-controller -f values.yaml -n vault-system --create-namespace
```

### 4. Verify the installation

```bash
kubectl get pods -n vault-system -l app.kubernetes.io/name=vault-namespace-controller
```

## Configuration Reference

The controller can be configured with the following options:

### Controller Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.reconcileInterval` | Reconciliation interval in seconds | `300` |
| `controller.deleteVaultNamespaces` | Whether to delete Vault namespaces when K8s namespaces are deleted | `true` |
| `controller.namespaceFormat` | Format string for Vault namespace names | `"%s"` |
| `controller.includeNamespaces` | Regular expressions for namespaces to include | `[]` |
| `controller.excludeNamespaces` | Regular expressions for namespaces to exclude. By default, the controller excludes Kubernetes system namespaces (kube-\*, openshift-\*, openshift, default) unless explicitly included. | `[]` |
| `controller.metricsBindAddress` | Metrics bind address | `":8080"` |
| `controller.leaderElection` | Whether to enable leader election | `true` |

### Vault Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `vault.address` | Vault server address (required) | `""` |
| `vault.namespaceRoot` | Vault namespace root (e.g., "/admin" for HCP Vault Dedicated) | `""` |
| `vault.caCert` | Path to CA certificate | `""` |
| `vault.clientCert` | Path to client certificate | `""` |
| `vault.clientKey` | Path to client key | `""` |
| `vault.insecure` | Whether to skip TLS verification (not recommended for production) | `false` |

### Authentication Methods

The controller supports three authentication methods to Vault:

#### 1. Kubernetes Auth Method

```yaml
vault:
  auth:
    type: "kubernetes"
    # Role to use for authentication (required)
    role: "vault-namespace-controller"
    # Optional: custom path where the kubernetes auth method is mounted
    path: "kubernetes"
    # Optional: namespace where the auth method resides
    namespace: ""
```

#### 2. Token Auth Method

```yaml
vault:
  auth:
    type: "token"
    # Direct token value (alternative to tokenPath)
    token: "hvs.CAESIEFf3pKHHLNw4NLRPxK9OZJrO4OcTYqJGrmPATLxkVjVGh4KHGh2cy43QlQyQzNuMVVRSGdZZHl3QnRlMVhwM3o"
    # OR: Path to a token file
    tokenPath: "/vault/token/secret"
```

#### 3. AppRole Auth Method

```yaml
vault:
  auth:
    type: "approle"
    # Direct credentials (alternative to paths)
    roleId: "role-id-value"
    secretId: "secret-id-value"
    # OR: Paths to credential files
    roleIdPath: "/vault/approle/role-id"
    secretIdPath: "/vault/approle/secret-id"
    # Optional: custom path where the approle auth method is mounted
    path: "approle"
```

## Example Configurations

### Basic configuration for vanilla Vault Enterprise with Kubernetes auth

```yaml
vault:
  address: "https://vault.example.com:8200"
  auth:
    type: "kubernetes"
    role: "vault-namespace-controller"

controller:
  namespaceFormat: "k8s-%s"
  excludeNamespaces:
    - "kube-.*"
    - "default"
```

### Configuration for HCP Vault Dedicated with token auth

```yaml
vault:
  address: "https://vault-cluster.vault.11eb1f78-54d6-fd10-5e17-0242ac11bd1d.aws.hashicorp.cloud:8200"
  namespaceRoot: "/admin"
  auth:
    type: "token"
    token: "hvs.CAESI..."

controller:
  namespaceFormat: "k8s-%s"
  deleteVaultNamespaces: false
```

### Configuration with TLS

```yaml
vault:
  address: "https://vault.example.com:8200"
  caCert: "/etc/vault/tls/ca.crt"
  clientCert: "/etc/vault/tls/client.crt"
  clientKey: "/etc/vault/tls/client.key"
  insecure: false
  
  auth:
    type: "kubernetes"
    role: "vault-namespace-controller"
```

### Configuration with AppRole auth and custom paths

```yaml
vault:
  address: "https://vault.example.com:8200"
  auth:
    type: "approle"
    path: "custom-approle"
    roleIdPath: "/etc/vault/auth/role-id"
    secretIdPath: "/etc/vault/auth/secret-id"
```

## Configuration with namespaces inclusion/exclusion patterns

```yaml
controller:
  # Only include namespaces matching these patterns
  includeNamespaces:
    - "^prod-.*"
    - "^staging-.*"
  
  # Exclude namespaces matching these patterns
  excludeNamespaces:
    - ".*-system$"
    - "kube-.*"
    - "default"

  # Format for Vault namespaces
  namespaceFormat: "k8s-%s"
```

## Troubleshooting

If you encounter issues with the controller, check the logs:

```bash
kubectl logs -l app.kubernetes.io/name=vault-namespace-controller -n vault-system
```

Common issues:

1. **Authentication failures**:
   - Verify the authentication configuration in Vault
   - Check that the service account has appropriate permissions
   - Confirm the role exists and has the right policies

2. **Permission issues**:
   - Ensure the authentication method has permissions to create/delete namespaces

3. **Network connectivity**:
   - Verify the Vault address is correct and accessible from the Kubernetes cluster
   - Check if TLS certificates are valid and trusted

4. **Configuration errors**:
   - Validate your values.yaml against the configuration reference
   - Ensure required fields for your chosen auth method are provided

## Upgrading

To upgrade the controller with a new configuration:

```bash
helm upgrade vault-namespace-controller ./deploy/helm/vault-namespace-controller -f values.yaml -n vault-system
```

## Uninstalling

To remove the controller:

```bash
helm uninstall vault-namespace-controller -n vault-system
```

Note that this will not remove any Vault namespaces that were created by the controller.
