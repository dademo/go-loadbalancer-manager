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

Important paths:
- local HAProxy runtime socket: `./tmp/haproxy/master.sock`
- local HAProxy config file: `./.devops/compose/haproxy.cfg`

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
- `make build`: build the multi-stage container image
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

- `.devops/compose/haproxy.cfg` does not expose an HTTP `/stats` page by default.
- HTTP traffic on `:8080` appears after creating a frontend dynamically through gRPC configuration.
