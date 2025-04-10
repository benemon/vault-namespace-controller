package controller

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/benemon/vault-namespace-controller/pkg/config"
	"github.com/benemon/vault-namespace-controller/pkg/metrics"
	"github.com/benemon/vault-namespace-controller/pkg/vault"
	"github.com/go-logr/logr"
)

var (
	ErrNamespaceCreation = errors.New("failed to create vault namespace")
	ErrNamespaceDeletion = errors.New("failed to delete vault namespace")
	ErrNamespaceCheck    = errors.New("failed to check vault namespace existence")
)

type NamespaceReconciler struct {
	client.Client
	Log         logr.Logger
	Scheme      *runtime.Scheme
	VaultClient vault.Client
	Config      *config.ControllerConfig
	syncChecker func(string) bool
}

func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	metrics.KubernetesEventsTotal.WithLabelValues("namespace").Inc()
	startTime := time.Now()

	// Format the Vault namespace path
	vaultNamespacePath := r.formatVaultNamespacePath(req.Name)

	// Create logger with both namespace contexts already added
	log := r.Log.WithValues(
		"kubernetesNamespace", req.Name,
		"vaultNamespace", vaultNamespacePath,
		"reconcileID", fmt.Sprintf("%d", startTime.UnixNano()),
	)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var namespace corev1.Namespace
	if err := r.Get(ctx, req.NamespacedName, &namespace); err != nil {
		if k8serrors.IsNotFound(err) {
			// Only log at INFO level for actual deletions
			if r.Config.DeleteVaultNamespaces {
				exists, _ := r.VaultClient.NamespaceExists(ctx, vaultNamespacePath)
				if exists {
					log.Info("Deleting Vault namespace")
				}
			}

			// Handle the deletion
			if err := r.handleNamespaceDeletion(ctx, vaultNamespacePath, log); err != nil {
				log.Error(err, "Failed to delete Vault namespace")
				metrics.ReconciliationTotal.WithLabelValues("error").Inc()
				metrics.ErrorsTotal.WithLabelValues("delete").Inc()
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}

			metrics.ReconciliationTotal.WithLabelValues("success").Inc()
			metrics.ReconciliationDuration.WithLabelValues("delete").Observe(time.Since(startTime).Seconds())
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Kubernetes namespace")
		metrics.ReconciliationTotal.WithLabelValues("error").Inc()
		metrics.ErrorsTotal.WithLabelValues("get").Inc()
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !r.shouldSyncNamespace(namespace.Name) {
		// Log exclusions at higher verbosity
		log.V(1).Info("Namespace excluded from synchronization",
			"includePatterns", r.Config.IncludeNamespaces,
			"excludePatterns", r.Config.ExcludeNamespaces)
		metrics.NamespacesExcluded.Set(1)
		return ctrl.Result{}, nil
	}

	// Before trying to create, check if it exists
	exists, _ := r.VaultClient.NamespaceExists(ctx, vaultNamespacePath)
	if !exists {
		log.Info("Creating Vault namespace")
	} else {
		// Only log routine reconciliations at higher verbosity
		log.V(1).Info("Reconciling existing namespace")
	}

	// Handle creation/reconciliation
	if err := r.handleNamespaceCreation(ctx, vaultNamespacePath, log); err != nil {
		log.Error(err, "Failed to create/reconcile Vault namespace")
		metrics.ReconciliationTotal.WithLabelValues("error").Inc()
		metrics.ErrorsTotal.WithLabelValues("create").Inc()
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	// Update metrics at higher verbosity
	log.V(2).Info("Updating namespace metrics")
	var nsList corev1.NamespaceList
	if err := r.Client.List(ctx, &nsList); err == nil {
		var managed, excluded, pending int
		for _, ns := range nsList.Items {
			if r.shouldSyncNamespace(ns.Name) {
				managed++
				vaultNS := r.formatVaultNamespacePath(ns.Name)
				exists, err := r.VaultClient.NamespaceExists(ctx, vaultNS)
				if err != nil || !exists {
					pending++
				}
			} else {
				excluded++
			}
		}
		metrics.NamespacesManaged.Set(float64(managed))
		metrics.NamespacesExcluded.Set(float64(excluded))
		metrics.NamespacesPendingSync.Set(float64(pending))
	}

	metrics.ReconciliationTotal.WithLabelValues("success").Inc()
	metrics.ReconciliationDuration.WithLabelValues("create").Observe(time.Since(startTime).Seconds())
	return ctrl.Result{RequeueAfter: time.Duration(r.Config.ReconcileInterval) * time.Second}, nil
}

func (r *NamespaceReconciler) shouldSyncNamespace(namespaceName string) bool {
	if r.syncChecker != nil {
		return r.syncChecker(namespaceName)
	}
	systemPatterns := []string{"^kube-.*", "^openshift-.*", "^openshift$", "^default$"}
	if matchesAnyPattern(namespaceName, systemPatterns) {
		return matchesAnyPattern(namespaceName, r.Config.IncludeNamespaces)
	}
	if matchesAnyPattern(namespaceName, r.Config.ExcludeNamespaces) {
		return false
	}
	if len(r.Config.IncludeNamespaces) > 0 {
		return matchesAnyPattern(namespaceName, r.Config.IncludeNamespaces)
	}
	return true
}

func matchesAnyPattern(name string, patterns []string) bool {
	for _, pattern := range patterns {
		if match, _ := regexp.MatchString(pattern, name); match {
			return true
		}
	}
	return false
}

// Update the handler methods to accept a logger parameter
func (r *NamespaceReconciler) handleNamespaceCreation(ctx context.Context, vaultNamespace string, log logr.Logger) error {

	exists, err := r.VaultClient.NamespaceExists(ctx, vaultNamespace)
	if err != nil {
		log.Error(err, "Failed to check if Vault namespace exists")
		return fmt.Errorf("%w: %v", ErrNamespaceCheck, err)
	}

	if !exists {
		// We already logged the creation in the main Reconcile function
		if err := r.VaultClient.CreateNamespace(ctx, vaultNamespace); err != nil {
			log.Error(err, "Failed to create Vault namespace")
			return fmt.Errorf("%w: %v", ErrNamespaceCreation, err)
		}
		log.V(1).Info("Successfully created Vault namespace")
	} else {
		log.V(2).Info("Vault namespace already exists")
	}

	return nil
}

func (r *NamespaceReconciler) handleNamespaceDeletion(ctx context.Context, vaultNamespace string, log logr.Logger) error {
	if !r.Config.DeleteVaultNamespaces {
		log.V(1).Info("Vault namespace deletion is disabled, skipping")
		return nil
	}

	exists, err := r.VaultClient.NamespaceExists(ctx, vaultNamespace)
	if err != nil {
		log.Error(err, "Failed to check if Vault namespace exists")
		return fmt.Errorf("%w: %v", ErrNamespaceCheck, err)
	}

	if exists {
		// We already logged the deletion in the main Reconcile function
		if err := r.VaultClient.DeleteNamespace(ctx, vaultNamespace); err != nil {
			log.Error(err, "Failed to delete Vault namespace")
			return fmt.Errorf("%w: %v", ErrNamespaceDeletion, err)
		}
		log.V(1).Info("Successfully deleted Vault namespace")
	} else {
		log.V(2).Info("Vault namespace does not exist, skipping deletion")
	}

	return nil
}

func (r *NamespaceReconciler) formatVaultNamespacePath(namespaceName string) string {
	formatted := namespaceName
	if r.Config.NamespaceFormat != "" {
		formatted = fmt.Sprintf(r.Config.NamespaceFormat, namespaceName)
	}
	if r.Config.Vault.NamespaceRoot != "" {
		nsRoot := strings.TrimRight(r.Config.Vault.NamespaceRoot, "/")
		formatted = fmt.Sprintf("%s/%s", nsRoot, strings.TrimLeft(formatted, "/"))
	}
	return formatted
}

func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Complete(r)
}
