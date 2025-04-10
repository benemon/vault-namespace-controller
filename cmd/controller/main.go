// Package main implements the entry point for the vault-namespace-controller.
package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"time"

	// Standard library imports
	"k8s.io/apimachinery/pkg/runtime"

	// Third-party imports
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	webhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	// Project imports
	"github.com/benemon/vault-namespace-controller/pkg/config"
	"github.com/benemon/vault-namespace-controller/pkg/controller"
	"github.com/benemon/vault-namespace-controller/pkg/vault"
)

// Common error definitions
var (
	ErrLoadConfig   = errors.New("unable to load controller configuration")
	ErrVaultClient  = errors.New("unable to create vault client")
	ErrManagerSetup = errors.New("unable to set up controller manager")
	ErrController   = errors.New("unable to create controller")
	ErrManagerStart = errors.New("problem running manager")
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
}

// main is the entry point for the vault-namespace-controller.
func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "Path to controller config file")

	opts := zap.Options{
		Development: false,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	logger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(logger)

	// Record start time for initialization metrics
	startTime := time.Now()

	setupLog.Info("Starting vault-namespace-controller",
		"version", getVersion(),
		"configPath", configPath)

	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		setupLog.Error(err, "Failed to load configuration",
			"configPath", configPath,
			"error", err.Error())
		os.Exit(1)
	}

	logConfig(cfg)

	// Create vault client
	setupLog.Info("Creating Vault client", "vaultAddress", cfg.Vault.Address)
	vaultClient, err := vault.NewClient(cfg.Vault)
	if err != nil {
		setupLog.Error(err, "Failed to create Vault client",
			"vaultAddress", cfg.Vault.Address,
			"error", err.Error())
		os.Exit(1)
	}
	setupLog.Info("Successfully connected to Vault")

	// Create context with graceful shutdown
	ctx, cancel := context.WithCancel(ctrl.SetupSignalHandler())
	defer cancel()

	// Create manager for controller
	setupLog.Info("Setting up controller manager")
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:         scheme,
		Metrics:        metricsserver.Options{BindAddress: cfg.MetricsBindAddress},
		WebhookServer:  webhook.NewServer(webhook.Options{Port: 9443}),
		LeaderElection: cfg.LeaderElection,
		// Use a more descriptive leader election ID
		LeaderElectionID: "vault-namespace-controller-leader",
	})
	if err != nil {
		setupLog.Error(err, "Failed to create controller manager",
			"error", err.Error())
		os.Exit(1)
	}

	// Create and set up the namespace controller
	setupLog.Info("Creating namespace controller")
	namespaceController := &controller.NamespaceReconciler{
		Client:      mgr.GetClient(),
		Log:         ctrl.Log.WithName("controllers").WithName("Namespace"),
		Scheme:      mgr.GetScheme(),
		VaultClient: vaultClient,
		Config:      cfg,
	}

	if err = namespaceController.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to set up controller",
			"controller", "Namespace",
			"error", err.Error())
		os.Exit(1)
	}

	// Log successful initialization and timing
	initDuration := time.Since(startTime)
	setupLog.Info("Controller initialization complete, starting manager",
		"initializationTime", initDuration.String(),
		"metricsBindAddress", cfg.MetricsBindAddress,
		"leaderElection", cfg.LeaderElection,
		"reconcileInterval", cfg.ReconcileInterval)

	// Start the controller
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "Problem running manager",
			"error", err.Error())
		os.Exit(1)
	}
}

// logConfig logs the controller configuration at startup
func logConfig(cfg *config.ControllerConfig) {
	setupLog.Info("Controller configuration",
		"reconcileInterval", cfg.ReconcileInterval,
		"deleteVaultNamespaces", cfg.DeleteVaultNamespaces,
		"namespaceFormat", cfg.NamespaceFormat,
		"includeNamespacesCount", len(cfg.IncludeNamespaces),
		"excludeNamespacesCount", len(cfg.ExcludeNamespaces),
		"metricsBindAddress", cfg.MetricsBindAddress,
		"leaderElection", cfg.LeaderElection)

	// Log Vault configuration without sensitive information
	setupLog.Info("Vault configuration",
		"address", cfg.Vault.Address,
		"namespaceRoot", cfg.Vault.NamespaceRoot,
		"authType", cfg.Vault.Auth.Type,
		"tlsConfigured", (cfg.Vault.CACert != "" || cfg.Vault.ClientCert != ""))
}

// getVersion returns the controller version
func getVersion() string {
	// This would typically be injected at build time via ldflags
	version := os.Getenv("VERSION")
	if version == "" {
		return "dev"
	}
	return version
}
