// Package main implements the entry point for the vault-namespace-controller.
package main

import (
	"flag"
	"os"

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
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		setupLog.Error(err, "Unable to load controller configuration", "configPath", configPath)
		os.Exit(1)
	}

	// Create vault client
	vaultClient, err := vault.NewClient(cfg.Vault)
	if err != nil {
		setupLog.Error(err, "Unable to create Vault client", "vaultAddress", cfg.Vault.Address)
		os.Exit(1)
	}

	// Create manager for controller
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:           scheme,
		Metrics:          metricsserver.Options{BindAddress: cfg.MetricsBindAddress},
		WebhookServer:    webhook.NewServer(webhook.Options{Port: 9443}),
		LeaderElection:   cfg.LeaderElection,
		LeaderElectionID: "vault-namespace-controller-leader",
	})
	if err != nil {
		setupLog.Error(err, "Unable to start manager")
		os.Exit(1)
	}

	// Create and set up the namespace controller
	namespaceController := &controller.NamespaceReconciler{
		Client:      mgr.GetClient(),
		Log:         ctrl.Log.WithName("controllers").WithName("Namespace"),
		Scheme:      mgr.GetScheme(),
		VaultClient: vaultClient,
		Config:      cfg,
	}

	if err = namespaceController.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Unable to create controller", "controller", "Namespace")
		os.Exit(1)
	}

	setupLog.Info("Starting manager",
		"metricsBindAddress", cfg.MetricsBindAddress,
		"leaderElection", cfg.LeaderElection,
		"reconcileInterval", cfg.ReconcileInterval)

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Problem running manager")
		os.Exit(1)
	}
}
