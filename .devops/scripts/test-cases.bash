#!/bin/bash
set -euo pipefail

GRPC_TARGET="localhost:50051"
HTTP_TARGET="http://localhost:8080/"
SKIP_UNIT=0

usage() {
  cat <<'EOF'
Usage: .devops/scripts/test-cases.bash [options]

Run functional test cases against a running go-loadbalancer-manager instance.

Options:
  --grpc-target <host:port>    gRPC endpoint (default: localhost:50051)
  --http-target <url>          LB URL to validate after configuration (default: http://localhost:8080/)
  --skip-unit                  Skip go test ./...
  -h, --help                   Show this help

Expected prerequisites:
  1) Service is running
  2) HAProxy + backends are running (make compose-up)
  3) grpcurl is installed
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --grpc-target)
      GRPC_TARGET="${2:-}"
      shift 2
      ;;
    --http-target)
      HTTP_TARGET="${2:-}"
      shift 2
      ;;
    --skip-unit)
      SKIP_UNIT=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if ! command -v grpcurl >/dev/null 2>&1; then
  echo "ERROR: grpcurl is required for functional tests." >&2
  echo "Install example: go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest" >&2
  exit 1
fi

if [[ "$SKIP_UNIT" -eq 0 ]]; then
  echo "[1/8] Running Go unit tests..."
  go test -v ./...
fi

echo "[2/8] Checking gRPC connectivity..."
grpcurl -plaintext "$GRPC_TARGET" list loadbalancer.v1.HaproxyStatusService >/dev/null

echo "[3/8] Creating HAProxy configuration (HTTP mode)..."
grpcurl -plaintext -d '{
  "configuration": {
    "name": "e2e-http",
    "frontend_name": "frontend_e2e_http",
    "frontend_bind_address": "0.0.0.0",
    "frontend_bind_port": 8080,
    "url": "http://localhost:8080/",
    "load_balancing_strategy": "HAPROXY_LOAD_BALANCING_STRATEGY_ROUNDROBIN",
    "backend_name": "backend_e2e_http",
    "backends": [
      {"name": "backend1", "address": "127.0.0.1", "port": 18001, "check_interval_seconds": 2},
      {"name": "backend2", "address": "127.0.0.1", "port": 18002, "check_interval_seconds": 2},
      {"name": "backend3", "address": "127.0.0.1", "port": 18003, "check_interval_seconds": 2}
    ],
    "traffic_mode": "HAPROXY_TRAFFIC_MODE_HTTP",
    "auto_https_redirect": false
  }
}' "$GRPC_TARGET" loadbalancer.v1.HaproxyStatusService/CreateConfiguration >/dev/null

echo "[4/8] Verifying created configuration exists..."
LIST_OUTPUT="$(grpcurl -plaintext -d '{}' "$GRPC_TARGET" loadbalancer.v1.HaproxyStatusService/ListConfigurations)"
printf '%s' "$LIST_OUTPUT" | grep -q '"name": "e2e-http"'

echo "[5/8] Running HTTP traffic checks..."
for i in 1 2 3 4 5; do
  code="$(curl -s -o /dev/null -w '%{http_code}' "$HTTP_TARGET")"
  if [[ "$code" != "200" ]]; then
    echo "ERROR: request $i returned HTTP $code" >&2
    exit 1
  fi
  echo "  request $i -> HTTP $code"
done

echo "[6/8] Running negative test (invalid configuration should fail)..."
if grpcurl -plaintext -d '{
  "configuration": {
    "name": "invalid-config",
    "frontend_name": "",
    "backend_name": "",
    "load_balancing_strategy": "HAPROXY_LOAD_BALANCING_STRATEGY_ROUNDROBIN",
    "traffic_mode": "HAPROXY_TRAFFIC_MODE_HTTP"
  }
}' "$GRPC_TARGET" loadbalancer.v1.HaproxyStatusService/CreateConfiguration >/dev/null 2>&1; then
  echo "ERROR: negative test failed (invalid config was accepted)" >&2
  exit 1
fi

echo "[7/8] Deleting test configuration..."
grpcurl -plaintext -d '{"name":"e2e-http"}' "$GRPC_TARGET" loadbalancer.v1.HaproxyStatusService/DeleteConfiguration >/dev/null

echo "[8/8] Ensuring cleanup is complete..."
LIST_OUTPUT_AFTER="$(grpcurl -plaintext -d '{}' "$GRPC_TARGET" loadbalancer.v1.HaproxyStatusService/ListConfigurations)"
if printf '%s' "$LIST_OUTPUT_AFTER" | grep -q '"name": "e2e-http"'; then
  echo "ERROR: configuration cleanup failed, e2e-http still present" >&2
  exit 1
fi

echo "All test cases passed successfully."
