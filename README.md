# Vault Namespace Controller

A Kubernetes controller that automatically synchronizes Kubernetes namespaces with HashiCorp Vault Enterprise namespaces.

## Overview

The Vault Namespace Controller watches for Kubernetes namespace events and creates or deletes corresponding namespaces in a Vault Enterprise instance. This enables seamless integration between Kubernetes and Vault Enterprise, ensuring that namespace isolation is consistent across both platforms.

## Key Features

- **Automatic Synchronization**: Creates and deletes Vault namespaces based on Kubernetes namespace lifecycle events
- **Flexible Configuration**: Customize how namespaces are mapped between Kubernetes and Vault
- **Multiple Authentication Methods**: Supports Kubernetes, Token, and AppRole authentication to Vault
- **HCP Vault Dedicated Support**: Works with cloud-hosted Vault instances including namespace roots
- **Namespace Filtering**: Include or exclude namespaces based on regular expression patterns
- **Enterprise-ready**: Includes leader election, metrics, and comprehensive logging

## How It Works

1. The controller watches Kubernetes namespace events (create, update, delete)
2. When a namespace is created, a corresponding Vault namespace is created
3. When a namespace is deleted, the corresponding Vault namespace can optionally be deleted
4. Namespace mapping can be customized with format strings and inclusion/exclusion patterns
5. By default, the controller excludes Kubernetes system namespaces (kube-\*, openshift-\*, openshift, default) unless explicitly included

## Installation

The controller can be installed using Helm:

```bash
helm install vault-namespace-controller ./deploy/helm/vault-namespace-controller \
  -f values.yaml \
  -n vault-system \
  --create-namespace
```

See the [Installation Guide](./docs/helm/installation.md) for detailed installation instructions.

## Configuration

Example configuration:

```yaml
# values.yaml
vault:
  address: "https://vault.example.com:8200"
  namespaceRoot: "/admin"  # For HCP Vault Dedicated
  auth:
    type: "kubernetes"
    role: "vault-namespace-controller"

controller:
  namespaceFormat: "k8s-%s"
  reconcileInterval: 300
  deleteVaultNamespaces: true
  excludeNamespaces:
    - "kube-.*"
    - "default"
```

See the [Configuration Reference](./docs/helm/installation.md#configuration-reference) for all available options.

## Requirements

- Kubernetes 1.16+
- Vault Enterprise with namespaces capability
- Go 1.24+

## Building

To build the controller:

```bash
make build
```

## License

This project is licensed under the [MIT License](LICENSE).

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.