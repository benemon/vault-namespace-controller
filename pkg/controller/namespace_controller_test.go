package controller

import (
	"context"
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/benemon/vault-namespace-controller/pkg/config"
)

// mockVaultClient is a mock implementation of the vault.Client interface
type mockVaultClient struct {
	mock.Mock
}

func (m *mockVaultClient) NamespaceExists(ctx context.Context, path string) (bool, error) {
	args := m.Called(ctx, path)
	return args.Bool(0), args.Error(1)
}

func (m *mockVaultClient) CreateNamespace(ctx context.Context, path string) error {
	args := m.Called(ctx, path)
	return args.Error(0)
}

func (m *mockVaultClient) DeleteNamespace(ctx context.Context, path string) error {
	args := m.Called(ctx, path)
	return args.Error(0)
}

func TestNamespaceReconciler_shouldSyncNamespace(t *testing.T) {
	tests := []struct {
		name           string
		namespaceName  string
		includePattern []string
		excludePattern []string
		expected       bool
	}{
		{
			name:          "default system namespace should not be synced",
			namespaceName: "kube-system",
			expected:      false,
		},
		{
			name:           "system namespace explicitly included should be synced",
			namespaceName:  "kube-system",
			includePattern: []string{"kube-.*"},
			expected:       true,
		},
		{
			name:           "namespace matching exclude pattern should not be synced",
			namespaceName:  "test-ns",
			excludePattern: []string{"test-.*"},
			expected:       false,
		},
		{
			name:           "namespace not matching include pattern should not be synced",
			namespaceName:  "test-ns",
			includePattern: []string{"prod-.*"},
			expected:       false,
		},
		{
			name:           "namespace matching include pattern should be synced",
			namespaceName:  "prod-ns",
			includePattern: []string{"prod-.*"},
			expected:       true,
		},
		{
			name:          "regular namespace should be synced by default",
			namespaceName: "app-namespace",
			expected:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal controller for testing shouldSyncNamespace
			r := &NamespaceReconciler{
				Config: &config.ControllerConfig{
					IncludeNamespaces: tt.includePattern,
					ExcludeNamespaces: tt.excludePattern,
				},
			}

			result := r.shouldSyncNamespace(tt.namespaceName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNamespaceReconciler_formatVaultNamespacePath(t *testing.T) {
	tests := []struct {
		name          string
		namespaceName string
		format        string
		namespaceRoot string
		expected      string
	}{
		{
			name:          "default format without root path",
			namespaceName: "test-ns",
			format:        "k8s-%s",
			namespaceRoot: "",
			expected:      "k8s-test-ns",
		},
		{
			name:          "custom format without root path",
			namespaceName: "test-ns",
			format:        "kubernetes-%s-ns",
			namespaceRoot: "",
			expected:      "kubernetes-test-ns-ns",
		},
		{
			name:          "default format with root path",
			namespaceName: "test-ns",
			format:        "k8s-%s",
			namespaceRoot: "/admin",
			expected:      "/admin/k8s-test-ns",
		},
		{
			name:          "handle trailing slash in root path",
			namespaceName: "test-ns",
			format:        "k8s-%s",
			namespaceRoot: "/admin/",
			expected:      "/admin/k8s-test-ns",
		},
		{
			name:          "handle leading slash in formatted name",
			namespaceName: "test-ns",
			format:        "/k8s-%s",
			namespaceRoot: "/admin",
			expected:      "/admin/k8s-test-ns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &NamespaceReconciler{
				Config: &config.ControllerConfig{
					NamespaceFormat: tt.format,
					Vault: config.VaultConfig{
						NamespaceRoot: tt.namespaceRoot,
					},
				},
			}

			result := r.formatVaultNamespacePath(tt.namespaceName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNamespaceReconciler_Reconcile(t *testing.T) {
	// Create a test logger
	testLogger := testr.New(t)

	// Set up the test scheme
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name              string
		namespace         *corev1.Namespace
		existingNamespace bool
		shouldSync        bool
		deleteEnabled     bool
		expectCreation    bool
		expectDeletion    bool
		setupMocks        bool // Whether to set up expectations for Vault client
	}{
		{
			name: "Should create Vault namespace when K8s namespace exists and should be synced",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-app",
				},
			},
			existingNamespace: false,
			shouldSync:        true,
			expectCreation:    true,
			expectDeletion:    false,
			setupMocks:        true, // We expect Vault client methods to be called
		},
		{
			name: "Should not create Vault namespace when it already exists",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "existing-app",
				},
			},
			existingNamespace: true,
			shouldSync:        true,
			expectCreation:    false,
			expectDeletion:    false,
			setupMocks:        true, // We expect Vault client methods to be called
		},
		{
			name: "Should not create Vault namespace when namespace should not be synced",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "kube-system",
				},
			},
			existingNamespace: false,
			shouldSync:        false,
			expectCreation:    false,
			expectDeletion:    false,
			setupMocks:        false, // We don't expect Vault client methods to be called
		},
		{
			name:              "Should delete Vault namespace when K8s namespace is deleted and delete is enabled",
			namespace:         nil, // Namespace not found, simulating deletion
			existingNamespace: true,
			shouldSync:        true,
			deleteEnabled:     true,
			expectCreation:    false,
			expectDeletion:    true,
			setupMocks:        true, // We expect Vault client methods to be called
		},
		{
			name:              "Should not delete Vault namespace when delete is disabled",
			namespace:         nil, // Namespace not found, simulating deletion
			existingNamespace: true,
			shouldSync:        true,
			deleteEnabled:     false,
			expectCreation:    false,
			expectDeletion:    false,
			setupMocks:        false, // We don't expect Vault client methods to be called
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client
			clientBuilder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.namespace != nil {
				clientBuilder = clientBuilder.WithObjects(tt.namespace)
			}

			// Build the fake client
			fakeClient := clientBuilder.Build()

			// Create mock Vault client
			mockClient := new(mockVaultClient)

			// Configure the mock expectations only if we expect them to be called
			if tt.setupMocks {
				vaultNamespaceName := "k8s-" // Will be completed based on the namespace
				if tt.namespace != nil {
					vaultNamespaceName += tt.namespace.Name
				} else {
					vaultNamespaceName += "deleted-ns" // Use a placeholder for deleted namespace test
				}

				// Set up the NamespaceExists expectation
				mockClient.On("NamespaceExists", mock.Anything, vaultNamespaceName).Return(tt.existingNamespace, nil)

				// Set up CreateNamespace expectation if needed
				if tt.expectCreation {
					mockClient.On("CreateNamespace", mock.Anything, vaultNamespaceName).Return(nil)
				}

				// Set up DeleteNamespace expectation if needed
				if tt.expectDeletion {
					mockClient.On("DeleteNamespace", mock.Anything, vaultNamespaceName).Return(nil)
				}
			}

			// Create a NamespaceReconciler with our mocks
			reconciler := &NamespaceReconciler{
				Client:      fakeClient,
				Log:         testLogger,
				Scheme:      scheme,
				VaultClient: mockClient,
				Config: &config.ControllerConfig{
					NamespaceFormat:       "k8s-%s",
					DeleteVaultNamespaces: tt.deleteEnabled,
				},
				// Use the syncChecker function field to control the shouldSyncNamespace behavior
				syncChecker: func(string) bool { return tt.shouldSync },
			}

			// Create a request
			var nsName string
			if tt.namespace != nil {
				nsName = tt.namespace.Name
			} else {
				nsName = "deleted-ns"
			}

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: nsName,
				},
			}

			// Call Reconcile
			_, err := reconciler.Reconcile(context.Background(), req)
			assert.NoError(t, err)

			// Assert that the expected methods were called
			mockClient.AssertExpectations(t)
		})
	}
}
