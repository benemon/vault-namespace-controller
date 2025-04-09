# Vault Namespace Controller Helm Chart

This directory contains a Helm chart for deploying the Vault Namespace Controller to a Kubernetes cluster.

## Chart Structure

```
vault-namespace-controller/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── _helpers.tpl
│   ├── deployment.yaml
│   ├── configmap.yaml
│   ├── secret.yaml
│   ├── serviceaccount.yaml
│   ├── clusterrole.yaml
│   ├── clusterrolebinding.yaml
│   └── NOTES.txt
└── .helmignore
```
