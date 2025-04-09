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
	log := r.Log.WithValues("namespace", req.Name, "reconcileID", fmt.Sprintf("%d", startTime.UnixNano()))
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var namespace corev1.Namespace
	if err := r.Get(ctx, req.NamespacedName, &namespace); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("Namespace not found, handling deletion")
			if err := r.handleNamespaceDeletion(ctx, req.Name); err != nil {
				metrics.ReconciliationTotal.WithLabelValues("error").Inc()
				metrics.ErrorsTotal.WithLabelValues("delete").Inc()
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			metrics.ReconciliationTotal.WithLabelValues("success").Inc()
			metrics.ReconciliationDuration.WithLabelValues("delete").Observe(time.Since(startTime).Seconds())
			return ctrl.Result{}, nil
		}
		metrics.ReconciliationTotal.WithLabelValues("error").Inc()
		metrics.ErrorsTotal.WithLabelValues("get").Inc()
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !r.shouldSyncNamespace(namespace.Name) {
		r.Log.V(1).Info("Namespace excluded from synchronization",
			"includePatterns", r.Config.IncludeNamespaces,
			"excludePatterns", r.Config.ExcludeNamespaces)
		metrics.NamespacesExcluded.Set(1)
		return ctrl.Result{}, nil
	}

	if err := r.handleNamespaceCreation(ctx, namespace.Name); err != nil {
		metrics.ReconciliationTotal.WithLabelValues("error").Inc()
		metrics.ErrorsTotal.WithLabelValues("create").Inc()
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

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

func (r *NamespaceReconciler) handleNamespaceCreation(ctx context.Context, namespaceName string) error {
	vaultNamespacePath := r.formatVaultNamespacePath(namespaceName)
	exists, err := r.VaultClient.NamespaceExists(ctx, vaultNamespacePath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNamespaceCheck, err)
	}
	if !exists {
		if err := r.VaultClient.CreateNamespace(ctx, vaultNamespacePath); err != nil {
			return fmt.Errorf("%w: %v", ErrNamespaceCreation, err)
		}
	}
	return nil
}

func (r *NamespaceReconciler) handleNamespaceDeletion(ctx context.Context, namespaceName string) error {
	if !r.Config.DeleteVaultNamespaces {
		return nil
	}
	vaultNamespacePath := r.formatVaultNamespacePath(namespaceName)
	exists, err := r.VaultClient.NamespaceExists(ctx, vaultNamespacePath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNamespaceCheck, err)
	}
	if exists {
		if err := r.VaultClient.DeleteNamespace(ctx, vaultNamespacePath); err != nil {
			return fmt.Errorf("%w: %v", ErrNamespaceDeletion, err)
		}
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
