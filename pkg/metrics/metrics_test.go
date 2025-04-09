package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestMetricsRegistration(t *testing.T) {
	// Verify that the metrics are registered correctly
	metrics := []prometheus.Collector{
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
	}

	for _, m := range metrics {
		assert.NotPanics(t, func() {
			prometheus.DefaultRegisterer.Unregister(m)
			prometheus.DefaultRegisterer.MustRegister(m)
		}, "metric should be registerable")
	}
}

func TestMetricsIncrement(t *testing.T) {
	// Reset the counters
	ReconciliationTotal.Reset()
	VaultOperationsTotal.Reset()
	ErrorsTotal.Reset()

	// Test incrementing counters
	ReconciliationTotal.WithLabelValues("success").Inc()
	value := testutil.ToFloat64(ReconciliationTotal.WithLabelValues("success"))
	assert.Equal(t, float64(1), value, "counter should be incremented")

	// Test adding to counters
	VaultOperationsTotal.WithLabelValues("create", "success").Add(2)
	value = testutil.ToFloat64(VaultOperationsTotal.WithLabelValues("create", "success"))
	assert.Equal(t, float64(2), value, "counter should be added to")

	// Test gauge setting
	NamespacesManaged.Set(42)
	value = testutil.ToFloat64(NamespacesManaged)
	assert.Equal(t, float64(42), value, "gauge should be set")

	// Test histogram observation
	ReconciliationDuration.WithLabelValues("create").Observe(0.1)
	// We can't directly test the histogram values here, but we can ensure it doesn't panic
}
