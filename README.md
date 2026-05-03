# go-loadbalancer-manager

Go service to manage HAProxy through gRPC.

The project exposes a gRPC API to:

- read HAProxy status (frontends/backends),
- create, list, get, update, and delete load-balancing configurations,
- write managed configurations into a dedicated block in the HAProxy config file.

## Overview

Main components:

- `cmd/main.go`: application entrypoint.
- `internal/controllers`: gRPC handlers.
- `internal/services`: business logic (HAProxy status, configuration CRUD, validation).
- `internal/repositories`: configuration loading and file/CLI access.
- `api/proto/loadbalancer/v1/haproxy_status.proto`: gRPC API contract.
- `.devops/compose`: local HAProxy + backend test environment.

HAProxy runtime is controlled through the Unix `master.sock` socket.

## Prerequisites

- Go `1.26`
- `make`
- Container runtime: `podman` (default) or `docker`
- Compose runtime: `podman-compose` (default) or `docker compose`
- Optional for functional tests: `grpcurl`

## Configuration

Default configuration:

- `internal/repositories/configuration/default.yaml`

Local dev configuration (used by Air and `LBM_CONFIG_ENV=dev`):

- `internal/repositories/configuration/environments/dev.yaml`

### Required Configuration

- **`haproxy.instance_name`** (required): Unique identifier for this load balancer instance. Used to namespace all managed configurations in the store backend. Example: `"lbm-prod"`, `"lbm-staging"`.

### Managed Configurations Persistence

Managed HAProxy configurations (created via gRPC) are persisted in a **store backend** that can be either:

1. **In-Memory** (default, for local development):
   - Configurations stored in-process memory
   - Lost on service restart
   - No external dependencies
   - Set `haproxy.managed_configurations_store.backend: "memory"`

2. **Redis** (for persistent, container-ephemeral deployments):
   - Configurations persisted to Redis, keyed by namespace
   - Survives service restarts; easily distributed across multiple instances
   - Requires Redis instance connectivity
   - Set `haproxy.managed_configurations_store.backend: "redis"` and configure Redis connection details
   - Namespace format: `haproxy:<normalized_instance_name>:configurations`

**Startup synchronization**: When the service starts:

1. If store backend is empty, configurations are read from the HAProxy managed block in the config file
2. Those configurations are seeded into the store backend (memory or Redis)
3. On each subsequent restart, the store backend becomes the source of truth

This design enables ephemeral containers (where the config file may be reset) to recover managed configurations from a persistent backend like Redis.

Important paths:

- local HAProxy runtime socket: `./tmp/haproxy/master.sock`
- local HAProxy config file (runtime, non versioned): `./tmp/haproxy/haproxy.cfg`
- HAProxy config source of truth (versioned): `./.devops/container/haproxy/haproxy.cfg`
- managed configurations namespace: `haproxy:<instance_name>` (in Redis or in-memory store)

## Useful Commands

Development:

- `make tidy`: format and tidy dependencies
- `make lint`: run lint checks
- `make test`: run unit tests
- `make run-dev`: run the service with `dev` environment config
- `make watch`: run with live reload via Air
- `make dev-local`: start compose services, then run Air

Compose (test environment):

- `make compose-up`
- `make compose-ps`
- `make compose-logs`
- `make compose-stats` (checks admin socket availability)
- `make compose-down`
- `make compose-clean`

Build:

- `make build` or `make build-app`: build the app multi-stage container image
- `make build-haproxy`: build the HAProxy image with embedded base config
- `make build-all`: build both app + HAProxy images (docker-bake equivalent, Podman compatible)
- `make extract`: extract the binary into `dist/`

## gRPC API

Service: `loadbalancer.v1.HaproxyStatusService`

Exposed RPCs:

- `GetStatus`
- `CreateConfiguration`
- `ListConfigurations`
- `GetConfiguration`
- `UpdateConfiguration`
- `DeleteConfiguration`

Full schema:

- `api/proto/loadbalancer/v1/haproxy_status.proto`

## Recommended Local Workflow

1. Start HAProxy + backends:
   - `make compose-up`
2. Run the service:
   - `make run-dev`
3. Verify gRPC availability (example):
   - `grpcurl -plaintext localhost:50051 list loadbalancer.v1.HaproxyStatusService`
4. Run functional test cases:
   - `make test-cases`

## Notes

- `.devops/container/haproxy/haproxy.cfg` does not expose an HTTP `/stats` page by default.
- HTTP traffic on `:8080` appears after creating a frontend dynamically through gRPC configuration.
- The HAProxy compose service uses a custom image (`go-loadbalancer-manager-haproxy:latest` by default) that embeds `.devops/container/haproxy/haproxy.cfg` at build time.
- `make compose-up` initializes `./tmp/haproxy/haproxy.cfg` from `.devops/container/haproxy/haproxy.cfg` if missing.
- `tmp/` is ignored by git, so local runtime config/state remains out of SCM.

### Redis Configuration

When using Redis for managed configuration persistence, configure via environment variables:

```bash
# Enable Redis backend
LBM_CFG_HAPROXY__MANAGED_CONFIGURATIONS_STORE__BACKEND=redis

# Set Redis connection details
LBM_CFG_HAPROXY__MANAGED_CONFIGURATIONS_STORE__REDIS__ADDRESS=redis:6379
LBM_CFG_HAPROXY__MANAGED_CONFIGURATIONS_STORE__REDIS__USERNAME=default    # optional
LBM_CFG_HAPROXY__MANAGED_CONFIGURATIONS_STORE__REDIS__PASSWORD=yourpass   # optional
LBM_CFG_HAPROXY__MANAGED_CONFIGURATIONS_STORE__REDIS__DB=0                # optional (default: 0)
```

### Configuration Namespace Format

All managed configurations are prefixed with `haproxy:<instance_name>:` in the store backend for isolation:

- Instance name: `"prod"` → Key prefix: `haproxy:prod:`
- Instance name: `"staging-1"` → Key prefix: `haproxy:staging_1:`
- Special characters in instance name are normalized to underscores
