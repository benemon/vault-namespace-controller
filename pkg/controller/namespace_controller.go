package controller

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/benemon/vault-namespace-controller/pkg/config"
	"github.com/benemon/vault-namespace-controller/pkg/vault"
	"github.com/go-logr/logr"
)

// Error definitions for the namespace controller
var (
	ErrNamespaceCreation = errors.New("failed to create vault namespace")
	ErrNamespaceDeletion = errors.New("failed to delete vault namespace")
	ErrNamespaceCheck    = errors.New("failed to check vault namespace existence")
)

// NamespaceReconciler reconciles a Namespace object.
type NamespaceReconciler struct {
	client.Client
	Log         logr.Logger
	Scheme      *runtime.Scheme
	VaultClient vault.Client
	Config      *config.ControllerConfig

	// SyncChecker is a function for testing
	syncChecker func(string) bool
}

// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch

// Reconcile handles the reconciliation logic for Kubernetes namespaces.
func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	log := r.Log.WithValues("namespace", req.Name, "reconcileID", fmt.Sprintf("%d", startTime.UnixNano()))

	log.Info("Starting namespace reconciliation")

	// Add a timeout to the context for this reconciliation
	ctx, cancel := context.WithTimeout(ctx, time.Duration(30)*time.Second)
	defer cancel()

	// Get the namespace
	var namespace corev1.Namespace
	if err := r.Get(ctx, req.NamespacedName, &namespace); err != nil {
		if k8serrors.IsNotFound(err) {
			// Namespace was deleted, try to delete corresponding Vault namespace
			log.Info("Namespace not found, handling deletion")
			if err := r.handleNamespaceDeletion(ctx, req.Name); err != nil {
				var retryAfter time.Duration = 30 * time.Second

				if errors.Is(err, ErrNamespaceDeletion) {
					log.Error(err, "Failed to delete Vault namespace",
						"vaultNamespace", r.formatVaultNamespacePath(req.Name),
						"retryAfter", retryAfter)
					return ctrl.Result{RequeueAfter: retryAfter}, err
				}

				// If it's a different error, just log it and don't retry
				log.Error(err, "Failed to handle namespace deletion with unexpected error")
				return ctrl.Result{}, nil
			}
			log.Info("Successfully handled namespace deletion")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get namespace from the API server")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if namespace should be synced based on inclusion/exclusion rules
	if !r.shouldSyncNamespace(namespace.Name) {
		log.V(1).Info("Namespace excluded from synchronization",
			"reason", "configuration rules",
			"includePatterns", r.Config.IncludeNamespaces,
			"excludePatterns", r.Config.ExcludeNamespaces)
		return ctrl.Result{}, nil
	}

	// Handle namespace creation or update
	if err := r.handleNamespaceCreation(ctx, namespace.Name); err != nil {
		var retryAfter time.Duration = 30 * time.Second

		if errors.Is(err, ErrNamespaceCreation) {
			log.Error(err, "Failed to create or update Vault namespace",
				"vaultNamespace", r.formatVaultNamespacePath(namespace.Name),
				"retryAfter", retryAfter)
			return ctrl.Result{RequeueAfter: retryAfter}, err
		}

		// If it's a different error, log it and don't retry
		log.Error(err, "Failed to handle namespace creation with unexpected error")
		return ctrl.Result{}, nil
	}

	reconcileDuration := time.Since(startTime)
	log.Info("Successfully reconciled namespace",
		"duration", reconcileDuration.String(),
		"vaultNamespace", r.formatVaultNamespacePath(namespace.Name))

	return ctrl.Result{}, nil
}

// matchesAnyPattern checks if a string matches any of the provided regex patterns.
func matchesAnyPattern(name string, patterns []string) bool {
	for _, pattern := range patterns {
		if match, _ := regexp.MatchString(pattern, name); match {
			return true
		}
	}
	return false
}

// shouldSyncNamespace checks if the namespace should be synced based on configuration rules.
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

	// Check if namespace is a system namespace
	isSystemNamespace := matchesAnyPattern(namespaceName, systemNamespacePatterns)

	// System namespaces are only included if explicitly listed in includeNamespaces
	if isSystemNamespace {
		r.Log.V(2).Info("System namespace detected",
			"namespace", namespaceName,
			"includeSystemNamespace", matchesAnyPattern(namespaceName, r.Config.IncludeNamespaces))
		return matchesAnyPattern(namespaceName, r.Config.IncludeNamespaces)
	}

	// Check exclude patterns - if matched, don't sync
	if matchesAnyPattern(namespaceName, r.Config.ExcludeNamespaces) {
		r.Log.V(2).Info("Namespace excluded by pattern",
			"namespace", namespaceName,
			"excludePatterns", r.Config.ExcludeNamespaces)
		return false
	}

	// If includeNamespaces is specified, only sync namespaces that match these patterns
	if len(r.Config.IncludeNamespaces) > 0 {
		matched := matchesAnyPattern(namespaceName, r.Config.IncludeNamespaces)
		r.Log.V(2).Info("Checking namespace against include patterns",
			"namespace", namespaceName,
			"includePatterns", r.Config.IncludeNamespaces,
			"matched", matched)
		return matched
	}

	// Default to include if not explicitly excluded
	r.Log.V(2).Info("Namespace included by default", "namespace", namespaceName)
	return true
}

// handleNamespaceCreation creates or updates a Vault namespace for the Kubernetes namespace.
func (r *NamespaceReconciler) handleNamespaceCreation(ctx context.Context, namespaceName string) error {
	vaultNamespacePath := r.formatVaultNamespacePath(namespaceName)
	log := r.Log.WithValues("k8sNamespace", namespaceName, "vaultNamespace", vaultNamespacePath)

	// Add a timeout for the Vault operation
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	log.V(1).Info("Checking if Vault namespace exists")

	exists, err := r.VaultClient.NamespaceExists(ctx, vaultNamespacePath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNamespaceCheck, err)
	}

	if !exists {
		log.Info("Creating Vault namespace")

		if err := r.VaultClient.CreateNamespace(ctx, vaultNamespacePath); err != nil {
			return fmt.Errorf("%w: %v", ErrNamespaceCreation, err)
		}

		log.Info("Successfully created Vault namespace")
	} else {
		log.V(1).Info("Vault namespace already exists")
	}

	return nil
}

// handleNamespaceDeletion deletes the corresponding Vault namespace.
func (r *NamespaceReconciler) handleNamespaceDeletion(ctx context.Context, namespaceName string) error {
	if !r.Config.DeleteVaultNamespaces {
		r.Log.Info("Vault namespace deletion is disabled, skipping",
			"k8sNamespace", namespaceName,
			"deleteVaultNamespaces", r.Config.DeleteVaultNamespaces)
		return nil
	}

	vaultNamespacePath := r.formatVaultNamespacePath(namespaceName)
	log := r.Log.WithValues("k8sNamespace", namespaceName, "vaultNamespace", vaultNamespacePath)

	// Add a timeout for the Vault operation
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	log.V(1).Info("Checking if Vault namespace exists for deletion")

	exists, err := r.VaultClient.NamespaceExists(ctx, vaultNamespacePath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNamespaceCheck, err)
	}

	if exists {
		log.Info("Deleting Vault namespace")

		if err := r.VaultClient.DeleteNamespace(ctx, vaultNamespacePath); err != nil {
			return fmt.Errorf("%w: %v", ErrNamespaceDeletion, err)
		}

		log.Info("Successfully deleted Vault namespace")
	} else {
		log.V(1).Info("Vault namespace doesn't exist, skipping deletion")
	}

	return nil
}

// formatVaultNamespacePath applies the namespace format pattern and prepends the root path if configured.
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

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Complete(r)
}
