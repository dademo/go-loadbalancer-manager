.DEFAULT_GOAL := help

APP_NAME := go-loadbalancer-manager
DIST_DIR := dist
APP_IMAGE_NAME ?= $(APP_NAME):latest
HAPROXY_IMAGE_NAME ?= $(APP_NAME)-haproxy:latest
APP_CONTAINERFILE ?= .devops/container/app/Containerfile
HAPROXY_CONTAINERFILE ?= .devops/container/haproxy/Containerfile
HAPROXY_SOURCE_CONFIG ?= .devops/container/haproxy/haproxy.cfg
HAPROXY_RUNTIME_DIR ?= tmp/haproxy
HAPROXY_RUNTIME_CONFIG ?= $(HAPROXY_RUNTIME_DIR)/haproxy.cfg
CONTAINER_RUNTIME ?= podman
COMPOSE_RUNTIME ?= podman-compose
VERSION_FILE ?= VERSION
VERSION ?= $(shell tr -d '\n' < $(VERSION_FILE) 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.Version=$(VERSION)

##@ General

.PHONY: help
help: ## Display this help menu
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Dependencies

.PHONY: install-deps
install-deps: ## Install golangci-lint and verify go tools
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install honnef.co/go/tools/cmd/staticcheck@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install github.com/air-verse/air@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	golangci-lint version

##@ Development

.PHONY: tidy
tidy: ## Format code and tidy dependencies
	go mod tidy
	goimports -w .
	go fmt ./...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run --config .golangci.yml ./...

.PHONY: lint-fix
lint-fix: ## Run golangci-lint and auto-fix issues
	golangci-lint run --config .golangci.yml --fix ./...

.PHONY: check
check: ## Run only staticcheck for deep analysis
	golangci-lint run --config .golangci.yml --enable-only staticcheck ./...

.PHONY: audit
audit: ## Scan for vulnerabilities in dependencies
	govulncheck ./...

.PHONY: test
test: ## Run unit tests
	go test -v -race ./...

.PHONY: proto
proto: ## Generate protobuf and gRPC Go code
	protoc -I. --go_out=. --go_opt=module=dademo.fr/loadbalancer-manager --go-grpc_out=. --go-grpc_opt=module=dademo.fr/loadbalancer-manager api/proto/loadbalancer/v1/haproxy_status.proto

.PHONY: run
run: tidy ## Run the application directly (use ARGS="foo" for params)
	go run ./cmd/main.go $(ARGS)

.PHONY: run-dev
run-dev: tidy ## Run the application directly (use ARGS="foo" for params)
	LBM_CONFIG_ENV=dev go run ./cmd/main.go $(ARGS)

.PHONY: configure-service
configure-service: ## Configure and run service locally (use ARGS="--env dev --no-run")
	./.devops/scripts/configure-service.bash $(ARGS)

.PHONY: test-cases
test-cases: ## Run functional test cases (requires running service + grpcurl)
	./.devops/scripts/test-cases.bash $(ARGS)

.PHONY: watch
watch: ## Run with live-reload (Air)
	air

.PHONY: dev-local
dev-local: compose-up watch ## Start development environment with HAProxy + backends and run the app in dev mode with air

.PHONY: version
version: ## Print resolved build version
	@echo $(VERSION)

##@ Build & Extract

.PHONY: build
build: build-app ## Build the app container image (backward compatible target)

.PHONY: build-app
build-app: ## Build the app container image
	DOCKER_BUILDKIT=1 $(CONTAINER_RUNTIME) build -t $(APP_IMAGE_NAME) -f $(APP_CONTAINERFILE) .

.PHONY: build-haproxy
build-haproxy: ## Build the HAProxy image with embedded base config
	DOCKER_BUILDKIT=1 $(CONTAINER_RUNTIME) build -t $(HAPROXY_IMAGE_NAME) -f $(HAPROXY_CONTAINERFILE) .

.PHONY: build-all
build-all: build-app build-haproxy ## Build app + HAProxy images (docker-bake equivalent)

.PHONY: extract
extract: build ## Build image and extract the binary to dist/
	@mkdir -p $(DIST_DIR)
	@id=$$($(CONTAINER_RUNTIME) create $(APP_IMAGE_NAME)); \
	$(CONTAINER_RUNTIME) cp $$id:/$(APP_NAME) $(DIST_DIR)/$(APP_NAME); \
	$(CONTAINER_RUNTIME) rm -v $$id > /dev/null
	@echo "Extracted to $(DIST_DIR)/$(APP_NAME)"

.PHONY: clean
clean: ## Clean up local dist artifacts and caches
	rm -rf $(DIST_DIR)
	go clean -testcache
	go clean -cache

##@ Docker Compose (Test Environment)

.PHONY: compose-prepare
compose-prepare: ## Prepare local compose runtime directories
	@mkdir -p $(HAPROXY_RUNTIME_DIR)
	@if [ ! -f $(HAPROXY_RUNTIME_CONFIG) ]; then \
		cp $(HAPROXY_SOURCE_CONFIG) $(HAPROXY_RUNTIME_CONFIG); \
		echo "Initialized local HAProxy config: ./$(HAPROXY_RUNTIME_CONFIG)"; \
	fi

.PHONY: compose-up
compose-up: compose-prepare build-haproxy ## Start HAProxy + backends $(COMPOSE_RUNTIME) -f .devops/compose/compose.yml environment
	$(COMPOSE_RUNTIME) -f .devops/compose/compose.yml up -d

.PHONY: compose-down
compose-down: ## Stop and remove all $(COMPOSE_RUNTIME) -f .devops/compose/compose.yml services
	$(COMPOSE_RUNTIME) -f .devops/compose/compose.yml down

.PHONY: compose-logs
compose-logs: ## Stream logs from $(COMPOSE_RUNTIME) -f .devops/compose/compose.yml services
	$(COMPOSE_RUNTIME) -f .devops/compose/compose.yml logs -f

.PHONY: compose-ps
compose-ps: ## Show running $(COMPOSE_RUNTIME) -f .devops/compose/compose.yml services
	$(COMPOSE_RUNTIME) -f .devops/compose/compose.yml ps

.PHONY: compose-stats
compose-stats: ## Check HAProxy admin socket status (no HTTP /stats by default)
	@if [ -S ./$(HAPROXY_RUNTIME_DIR)/admin.sock ]; then \
		echo "HAProxy admin socket is available: ./$(HAPROXY_RUNTIME_DIR)/admin.sock"; \
		echo "No default HTTP /stats endpoint is configured on :8080."; \
	else \
		echo "HAProxy admin socket not found: ./$(HAPROXY_RUNTIME_DIR)/admin.sock"; \
		echo "Start dependencies first with: make compose-up"; \
		exit 1; \
	fi

.PHONY: compose-test-lb
compose-test-lb: ## Test load balancing with 10 requests
	@echo "Testing load balancing (10 requests)..."
	@for i in {1..10}; do \
		echo "\n[Request $$i]"; \
		curl -s -w "HTTP Status: %{http_code}\n" http://localhost:8080/ | head -20; \
	done

.PHONY: compose-clean
compose-clean: ## Remove $(COMPOSE_RUNTIME) -f .devops/compose/compose.yml volumes and networks
	$(COMPOSE_RUNTIME) -f .devops/compose/compose.yml down -v
