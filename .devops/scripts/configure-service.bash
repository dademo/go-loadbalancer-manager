#!/bin/bash
set -euo pipefail

ENVIRONMENT="dev"
CONFIG_FILE=""
GRPC_ADDRESS=""
HAPROXY_SOCKET_ADDRESS=""
NO_COMPOSE=0
NO_RUN=0

usage() {
  cat <<'EOF'
Usage: .devops/scripts/configure-service.bash [options] [-- <app extra args>]

Configure and optionally run go-loadbalancer-manager with local HAProxy dependencies.
Build is performed through Make targets using the Containerfile.

Options:
  --env <name>                 Embedded environment config (default: dev)
  --config <path>              External YAML config file passed with -config
  --grpc-address <address>     Override grpc.address via LBM_CFG_GRPC__ADDRESS
  --haproxy-socket <path>      Override haproxy.socket.address via LBM_CFG_HAPROXY__SOCKET__ADDRESS
  --no-compose                 Do not start compose dependencies
  --no-run                     Do not run the Go service, only prepare environment
  -h, --help                   Show this help

Examples:
  .devops/scripts/configure-service.bash
  .devops/scripts/configure-service.bash --env dev --grpc-address :50055
  .devops/scripts/configure-service.bash --config ./configuration/environments/local.yaml -- --debug
EOF
}

APP_ARGS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --env)
      ENVIRONMENT="${2:-}"
      shift 2
      ;;
    --config)
      CONFIG_FILE="${2:-}"
      shift 2
      ;;
    --grpc-address)
      GRPC_ADDRESS="${2:-}"
      shift 2
      ;;
    --haproxy-socket)
      HAPROXY_SOCKET_ADDRESS="${2:-}"
      shift 2
      ;;
    --no-compose)
      NO_COMPOSE=1
      shift
      ;;
    --no-run)
      NO_RUN=1
      shift
      ;;
    --)
      shift
      APP_ARGS=("$@")
      break
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      APP_ARGS+=("$1")
      shift
      ;;
  esac
done

if [[ -n "$CONFIG_FILE" && ! -f "$CONFIG_FILE" ]]; then
  echo "ERROR: config file not found: $CONFIG_FILE" >&2
  exit 1
fi

mkdir -p ./tmp/haproxy

if [[ "$NO_COMPOSE" -eq 0 ]]; then
  echo "[1/4] Starting local dependencies (HAProxy + backends)..."
  make compose-up
fi

echo "[2/4] Applying configuration..."
export LBM_CONFIG_ENV="$ENVIRONMENT"

if [[ -n "$GRPC_ADDRESS" ]]; then
  export LBM_CFG_GRPC__ADDRESS="$GRPC_ADDRESS"
fi

if [[ -n "$HAPROXY_SOCKET_ADDRESS" ]]; then
  export LBM_CFG_HAPROXY__SOCKET__ADDRESS="$HAPROXY_SOCKET_ADDRESS"
fi

echo "Configuration summary:"
echo "  LBM_CONFIG_ENV=$LBM_CONFIG_ENV"
[[ -n "${LBM_CFG_GRPC__ADDRESS:-}" ]] && echo "  LBM_CFG_GRPC__ADDRESS=$LBM_CFG_GRPC__ADDRESS"
[[ -n "${LBM_CFG_HAPROXY__SOCKET__ADDRESS:-}" ]] && echo "  LBM_CFG_HAPROXY__SOCKET__ADDRESS=$LBM_CFG_HAPROXY__SOCKET__ADDRESS"
[[ -n "$CONFIG_FILE" ]] && echo "  -config $CONFIG_FILE"

if [[ "$NO_RUN" -eq 1 ]]; then
  echo "[3/4] Environment prepared. Service not started (--no-run)."
  exit 0
fi

echo "[3/4] Building binary through Containerfile (make extract)..."
make extract

APP_BINARY="./dist/go-loadbalancer-manager"
if [[ ! -x "$APP_BINARY" ]]; then
  echo "ERROR: expected executable not found after build: $APP_BINARY" >&2
  exit 1
fi

echo "[4/4] Running go-loadbalancer-manager binary..."
if [[ -n "$CONFIG_FILE" ]]; then
  "$APP_BINARY" -config "$CONFIG_FILE" "${APP_ARGS[@]}"
else
  "$APP_BINARY" "${APP_ARGS[@]}"
fi
