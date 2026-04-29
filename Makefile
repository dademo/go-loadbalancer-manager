.DEFAULT_GOAL := help

APP_NAME := go-loadbalancer-manager
DIST_DIR := dist
IMAGE_NAME := $(APP_NAME):latest
RUNTIME ?= podman
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
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install honnef.co/go/tools/cmd/staticcheck@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install github.com/air-verse/air@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

##@ Development

.PHONY: tidy
tidy: ## Format code and tidy dependencies
	go mod tidy
	goimports -w .
	go fmt ./...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run ./...

.PHONY: lint-fix
lint-fix: ## Run golangci-lint and auto-fix issues
	golangci-lint run --fix ./...

.PHONY: check
check: ## Run only staticcheck for deep analysis
	golangci-lint run --disable-all -E staticcheck ./...

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
	go run . $(ARGS)

.PHONY: watch
watch: ## Run with live-reload (Air)
	air

.PHONY: version
version: ## Print resolved build version
	@echo $(VERSION)

##@ Build & Extract

.PHONY: build
build: ## Build the multi-layered container image
	DOCKER_BUILDKIT=1 $(RUNTIME) build -t $(IMAGE_NAME) -f .devops/container/Containerfile .

.PHONY: extract
extract: build ## Build image and extract the binary to dist/
	@mkdir -p $(DIST_DIR)
	@id=$$($(RUNTIME) create $(IMAGE_NAME)); \
	$(RUNTIME) cp $$id:/$(APP_NAME) $(DIST_DIR)/$(APP_NAME); \
	$(RUNTIME) rm -v $$id > /dev/null
	@echo "Extracted to $(DIST_DIR)/$(APP_NAME)"

.PHONY: clean
clean: ## Clean up local dist artifacts and caches
	rm -rf $(DIST_DIR)
	go clean -testcache
	go clean -cache
