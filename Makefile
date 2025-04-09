# Makefile for vault-namespace-controller

# Variables
BINARY_NAME := vault-namespace-controller
IMAGE_NAME := vault-namespace-controller
TAG ?= latest
REGISTRY ?= ghcr.io/benemon
PLATFORMS ?= linux/amd64,linux/arm64

# Go variables
GO := go
GO_BUILD := $(GO) build
GO_TEST := $(GO) test
GO_FMT := $(GO) fmt
GO_PACKAGES := ./cmd/... ./pkg/...
GO_FILES := $(shell find . -name "*.go" -not -path "./vendor/*")
GO_LDFLAGS := -ldflags "-X main.version=$(TAG) -s -w"

# Local directory to store artifacts
BIN_DIR := bin
DIST_DIR := dist

# Linting & code analysis tools
GOLANGCI_LINT := golangci-lint

# Container tools
CONTAINER_BUILDER ?= podman
CONTAINER_FILE ?= Containerfile

.PHONY: all
all: fmt lint test build

# Build the application
.PHONY: build
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BIN_DIR)
	$(GO_BUILD) $(GO_LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME) cmd/controller/main.go

# Run tests
.PHONY: test
test:
	@echo "Running tests..."
	$(GO_TEST) -v -race -cover $(GO_PACKAGES)

# Run tests with coverage report
.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	@mkdir -p $(DIST_DIR)
	$(GO_TEST) -v -race -coverprofile=$(DIST_DIR)/coverage.out -covermode=atomic $(GO_PACKAGES)
	$(GO) tool cover -html=$(DIST_DIR)/coverage.out -o $(DIST_DIR)/coverage.html

# Format code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	$(GO_FMT) $(GO_PACKAGES)

# Lint code
.PHONY: lint
lint:
	@echo "Linting code..."
	$(GOLANGCI_LINT) run

# Tidy modules
.PHONY: tidy
tidy:
	@echo "Tidying modules..."
	$(GO) mod tidy

# Build container image
.PHONY: container-build
container-build:
	@echo "Building container image $(REGISTRY)/$(IMAGE_NAME):$(TAG)..."
	$(CONTAINER_BUILDER) build -t $(REGISTRY)/$(IMAGE_NAME):$(TAG) -f $(CONTAINER_FILE) .

# Push container image
.PHONY: container-push
container-push:
	@echo "Pushing container image $(REGISTRY)/$(IMAGE_NAME):$(TAG)..."
	$(CONTAINER_BUILDER) push $(REGISTRY)/$(IMAGE_NAME):$(TAG)

# Build multi-platform container images
.PHONY: container-buildx
container-buildx:
	@echo "Building multi-platform container image $(REGISTRY)/$(IMAGE_NAME):$(TAG)..."
	$(CONTAINER_BUILDER) buildx build --platform $(PLATFORMS) \
		-t $(REGISTRY)/$(IMAGE_NAME):$(TAG) \
		-f $(CONTAINER_FILE) .

# Create release artifacts
.PHONY: release
release: 
	@echo "Creating release artifacts..."
	@mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=amd64 $(GO_BUILD) $(GO_LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 cmd/controller/main.go
	GOOS=linux GOARCH=arm64 $(GO_BUILD) $(GO_LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 cmd/controller/main.go
	GOOS=darwin GOARCH=amd64 $(GO_BUILD) $(GO_LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 cmd/controller/main.go
	GOOS=darwin GOARCH=arm64 $(GO_BUILD) $(GO_LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 cmd/controller/main.go

# Run the application
.PHONY: run
run: build
	@echo "Running $(BINARY_NAME)..."
	./$(BIN_DIR)/$(BINARY_NAME) --config=config.yaml

# Clean up
.PHONY: clean
clean:
	@echo "Cleaning up..."
	@rm -rf $(BIN_DIR) $(DIST_DIR)
	@echo "Clean complete"

# Generate manifests
.PHONY: manifests
manifests:
	@echo "Generating Kubernetes manifests..."
	@mkdir -p deploy/kubernetes
	# Add your manifest generation commands here

# Install dependencies
.PHONY: deps
deps:
	@echo "Installing dependencies..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Help target
.PHONY: help
help:
	@echo "Makefile for $(BINARY_NAME)"
	@echo ""
	@echo "Usage:"
	@echo "  make <target>"
	@echo ""
	@echo "Targets:"
	@echo "  all             Run fmt, lint, test, and build"
	@echo "  build           Build the application"
	@echo "  test            Run tests"
	@echo "  test-coverage   Run tests with coverage report"
	@echo "  fmt             Format code"
	@echo "  lint            Lint code"
	@echo "  tidy            Tidy Go modules"
	@echo "  container-build Build container image"
	@echo "  container-push  Push container image to registry"
	@echo "  container-buildx Build multi-platform container images"
	@echo "  release         Create release artifacts"
	@echo "  run             Run the application"
	@echo "  clean           Clean up build artifacts"
	@echo "  manifests       Generate Kubernetes manifests"
	@echo "  deps            Install dependencies"
	@echo "  help            Show this help message"