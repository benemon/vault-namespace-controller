// Package metrics provides Prometheus metrics for the vault-namespace-controller.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Define metrics variables
var (
	// Reconciliation metrics
	ReconciliationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vault_ns_controller_reconciliation_total",
			Help: "Total number of reconciliation attempts",
		},
		[]string{"result"},
	)

	ReconciliationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "vault_ns_controller_reconciliation_duration_seconds",
			Help:    "Time taken to complete reconciliations",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	// Vault operation metrics
	VaultOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vault_ns_controller_vault_operations_total",
			Help: "Total number of Vault operations performed",
		},
		[]string{"operation", "result"},
	)

	VaultOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "vault_ns_controller_vault_operation_duration_seconds",
			Help:    "Time taken for Vault API operations",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	// Namespace tracking metrics
	NamespacesManaged = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "vault_ns_controller_namespaces_managed_total",
			Help: "Total number of namespaces being managed",
		},
	)

	NamespacesExcluded = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "vault_ns_controller_namespaces_excluded_total",
			Help: "Number of namespaces excluded by rules",
		},
	)

	// Connection status
	VaultConnectionUp = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "vault_ns_controller_vault_connection_up",
			Help: "Vault connection status (0 for down, 1 for up)",
		},
	)

	// Authentication metrics
	VaultTokenTTL = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "vault_ns_controller_vault_token_ttl_seconds",
			Help: "Remaining TTL of the Vault token in seconds",
		},
	)

	// Error metrics by type
	ErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vault_ns_controller_errors_total",
			Help: "Total number of errors by type",
		},
		[]string{"type"},
	)

	// Leader election metrics (if using controller-runtime's leader election)
	IsLeader = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "vault_ns_controller_is_leader",
			Help: "Whether this instance is the leader (0 or 1)",
		},
	)

	LeaderElectionTransitions = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "vault_ns_controller_leader_election_transitions_total",
			Help: "Number of leader transitions",
		},
	)

	// Pending synchronization
	NamespacesPendingSync = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "vault_ns_controller_namespaces_pending_sync",
			Help: "Number of namespaces pending Vault synchronization due to failures",
		},
	)

	// Vault authentication metrics
	VaultAuthOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vault_ns_controller_vault_auth_operations_total",
			Help: "Total number of Vault authentication operations",
		},
		[]string{"auth_method"},
	)

	VaultAuthErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vault_ns_controller_vault_auth_errors_total",
			Help: "Total number of Vault authentication failures",
		},
		[]string{"auth_method"},
	)

	VaultAuthDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "vault_ns_controller_vault_auth_duration_seconds",
			Help:    "Time taken for Vault authentication operations",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"auth_method"},
	)

	// Kubernetes event processing
	KubernetesEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vault_ns_controller_kubernetes_event_processing_total",
			Help: "Number of Kubernetes events processed by the controller",
		},
		[]string{"resource"},
	)
)

func init() {
	// Register metrics with the controller-runtime manager
	metrics.Registry.MustRegister(
		ReconciliationTotal,
		ReconciliationDuration,
		VaultOperationsTotal,
		VaultOperationDuration,
		NamespacesManaged,
		NamespacesExcluded,
		VaultConnectionUp,
		VaultTokenTTL,
		ErrorsTotal,
		IsLeader,
		LeaderElectionTransitions,
		NamespacesPendingSync,
		VaultAuthOperationsTotal,
		VaultAuthErrorsTotal,
		VaultAuthDuration,
		KubernetesEventsTotal,
	)
}
