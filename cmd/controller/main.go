package main

import (
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/benemon/vault-namespace-controller/pkg/config"
	"github.com/benemon/vault-namespace-controller/pkg/controller"
	"github.com/benemon/vault-namespace-controller/pkg/vault"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	webhook "sigs.k8s.io/controller-runtime/pkg/webhook"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
}

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
		setupLog.Error(err, "unable to load controller configuration")
		os.Exit(1)
	}

	// Create vault client
	vaultClient, err := vault.NewClient(cfg.Vault)
	if err != nil {
		setupLog.Error(err, "unable to create Vault client")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:           scheme,
		Metrics:          metricsserver.Options{BindAddress: cfg.MetricsBindAddress},
		WebhookServer:    webhook.NewServer(webhook.Options{Port: 9443}),
		LeaderElection:   cfg.LeaderElection,
		LeaderElectionID: "vault-namespace-controller-leader",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controller.NamespaceReconciler{
		Client:      mgr.GetClient(),
		Log:         ctrl.Log.WithName("controllers").WithName("Namespace"),
		Scheme:      mgr.GetScheme(),
		VaultClient: vaultClient,
		Config:      cfg,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Namespace")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
