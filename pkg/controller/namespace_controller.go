package controller

import (
	"context"
	"fmt"
	"regexp"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/benemon/vault-namespace-controller/pkg/config"
	"github.com/benemon/vault-namespace-controller/pkg/vault"
	"github.com/go-logr/logr"
)

// NamespaceReconciler reconciles a Namespace object
type NamespaceReconciler struct {
	client.Client
	Log         logr.Logger
	Scheme      *runtime.Scheme
	VaultClient vault.Client
	Config      *config.ControllerConfig

	// Add a function field for testing
	syncChecker func(string) bool
}

// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch

// Reconcile handles the reconciliation logic for Kubernetes namespaces
func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("namespace", req.Name)
	log.Info("Reconciling namespace")

	// Get the namespace
	var namespace corev1.Namespace
	if err := r.Get(ctx, req.NamespacedName, &namespace); err != nil {
		if errors.IsNotFound(err) {
			// Namespace was deleted, try to delete corresponding Vault namespace
			log.Info("Namespace not found, likely deleted")
			if err := r.handleNamespaceDeletion(ctx, req.Name); err != nil {
				log.Error(err, "Failed to delete Vault namespace")
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get namespace")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if namespace should be synced based on inclusion/exclusion rules
	if !r.shouldSyncNamespace(namespace.Name) {
		log.Info("Namespace excluded from synchronization by configuration rules")
		return ctrl.Result{}, nil
	}

	// Handle namespace creation or update
	if err := r.handleNamespaceCreation(ctx, namespace.Name); err != nil {
		log.Error(err, "Failed to create or update Vault namespace")
		return ctrl.Result{RequeueAfter: time.Second * 30}, err
	}

	log.Info("Successfully reconciled namespace")
	return ctrl.Result{}, nil
}

// shouldSyncNamespace checks if the namespace should be synced based on configuration rules
func (r *NamespaceReconciler) shouldSyncNamespace(namespaceName string) bool {
	// If syncChecker is set (for testing), use that
	if r.syncChecker != nil {
		return r.syncChecker(namespaceName)
	}

	// Define system namespace patterns
	systemNamespacePatterns := []string{
		"^kube-.*",      // All kube- prefixed namespaces
		"^openshift-.*", // All openshift- prefixed namespaces
		"^openshift$",   // The base openshift namespace
		"^default$",     // The default namespace
	}

	// Check if namespace matches any system namespace pattern and is not explicitly included
	for _, pattern := range systemNamespacePatterns {
		if match, _ := regexp.MatchString(pattern, namespaceName); match {
			// Check if explicitly included
			for _, includePattern := range r.Config.IncludeNamespaces {
				if match, _ := regexp.MatchString(includePattern, namespaceName); match {
					return true
				}
			}
			return false
		}
	}

	// Check exclude patterns
	for _, pattern := range r.Config.ExcludeNamespaces {
		if match, _ := regexp.MatchString(pattern, namespaceName); match {
			return false
		}
	}

	// Check include patterns if specified
	if len(r.Config.IncludeNamespaces) > 0 {
		for _, pattern := range r.Config.IncludeNamespaces {
			if match, _ := regexp.MatchString(pattern, namespaceName); match {
				return true
			}
		}
		return false
	}

	// Default to include if not explicitly excluded
	return true
}

// handleNamespaceCreation creates or updates a Vault namespace for the Kubernetes namespace
func (r *NamespaceReconciler) handleNamespaceCreation(ctx context.Context, namespaceName string) error {
	vaultNamespacePath := r.formatVaultNamespacePath(namespaceName)
	exists, err := r.VaultClient.NamespaceExists(ctx, vaultNamespacePath)
	if err != nil {
		return fmt.Errorf("failed to check if Vault namespace exists: %w", err)
	}

	if !exists {
		if err := r.VaultClient.CreateNamespace(ctx, vaultNamespacePath); err != nil {
			return fmt.Errorf("failed to create Vault namespace: %w", err)
		}
		r.Log.Info("Created Vault namespace", "vaultNamespace", vaultNamespacePath)
	} else {
		r.Log.Info("Vault namespace already exists", "vaultNamespace", vaultNamespacePath)
	}

	return nil
}

// handleNamespaceDeletion deletes the corresponding Vault namespace
func (r *NamespaceReconciler) handleNamespaceDeletion(ctx context.Context, namespaceName string) error {
	if !r.Config.DeleteVaultNamespaces {
		r.Log.Info("Vault namespace deletion is disabled, skipping", "k8sNamespace", namespaceName)
		return nil
	}

	vaultNamespacePath := r.formatVaultNamespacePath(namespaceName)
	exists, err := r.VaultClient.NamespaceExists(ctx, vaultNamespacePath)
	if err != nil {
		return fmt.Errorf("failed to check if Vault namespace exists: %w", err)
	}

	if exists {
		if err := r.VaultClient.DeleteNamespace(ctx, vaultNamespacePath); err != nil {
			return fmt.Errorf("failed to delete Vault namespace: %w", err)
		}
		r.Log.Info("Deleted Vault namespace", "vaultNamespace", vaultNamespacePath)
	} else {
		r.Log.Info("Vault namespace doesn't exist, skipping deletion", "vaultNamespace", vaultNamespacePath)
	}

	return nil
}

// formatVaultNamespacePath applies the namespace format pattern and prepends the root path if configured
func (r *NamespaceReconciler) formatVaultNamespacePath(namespaceName string) string {
	// Apply namespace format pattern if configured
	formattedName := namespaceName
	if r.Config.NamespaceFormat != "" {
		formattedName = fmt.Sprintf(r.Config.NamespaceFormat, namespaceName)
	}

	// Prepend namespace root if configured
	if r.Config.Vault.NamespaceRoot != "" {
		// Ensure there's a single / between namespace root and namespace
		nsRoot := r.Config.Vault.NamespaceRoot
		if nsRoot[len(nsRoot)-1] == '/' {
			nsRoot = nsRoot[:len(nsRoot)-1]
		}

		if formattedName[0] == '/' {
			formattedName = formattedName[1:]
		}

		return fmt.Sprintf("%s/%s", nsRoot, formattedName)
	}

	return formattedName
}

// SetupWithManager sets up the controller with the Manager
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Complete(r)
}
