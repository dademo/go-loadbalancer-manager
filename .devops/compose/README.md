# HAProxy + Backends Docker Compose Setup

This compose configuration creates a complete test environment for the Go load balancer manager with HAProxy.

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│ Host Machine (localhost)                                │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │ Go Load Balancer Manager (cmd/main.go)            │ │
│  │ - Connects to: /var/run/haproxy/master.sock       │ │
│  │ - Queries HAProxy via client-native/v6            │ │
│  └────────────────────────────────────────────────────┘ │
│            ↓ Unix socket (volume mount)                 │
├─────────────────────────────────────────────────────────┤
│ Docker Compose                                          │
│                                                          │
│  ┌─ Internal Network (haproxy-internal) ─────────────┐ │
│  │                                                    │ │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────┐ │ │
│  │  │ backend-1    │  │ backend-2    │  │backend-3 │ │ │
│  │  │ :8001        │  │ :8002        │  │:8003     │ │ │
│  │  └──────────────┘  └──────────────┘  └──────────┘ │ │
│  │         ↑               ↑                ↑          │ │
│  │         └───────────────┼────────────────┘          │ │
│  │                         ↓                           │ │
│  │              ┌──────────────────┐                   │ │
│  │              │   HAProxy        │                   │ │
│  │              │  Load Balancer   │                   │ │
│  │              │                  │                   │ │
│  │              │ master.sock      │ ←─────────────┐   │ │
│  │              │ admin.sock       │ (healthcheck) │   │ │
│  │              └──────────────────┘               │   │ │
│  │                                                  │   │ │
│  └──────────────────────────────────────────────────┼───┘ │
│           ↓ optional traffic on 8080     │            │
│                                         │            │
│    ┌──────────────────────────────┐    │            │
│    │ Host Network Bridge          │────┘            │
│    │ (volume mount admin.sock)    │                 │
│    └──────────────────────────────┘                 │
│                                                      │
│    Socket available at:                             │
│    ./tmp/haproxy/master.sock, ./tmp/haproxy/admin.sock │
└─────────────────────────────────────────────────────────┘
```

## Services

### Backends (backend-1, backend-2, backend-3)

- **Image**: `python:3.11-alpine`
- **Ports**: 8001, 8002, 8003 (on internal network)
- **Function**: Simple HTTP servers for load balancing testing
- **Health**: HTTP health checks every 5s

### HAProxy

- **Image**: `go-loadbalancer-manager-haproxy:latest` (or `HAPROXY_IMAGE` override)
- **Networks**: Internal (`haproxy-internal`) + Host
- **Master Socket**: `/var/run/haproxy/master.sock` (used by the Go runtime client)
- **Admin Socket**: `/var/run/haproxy/admin.sock` (health/admin checks)
- **HTTP Listener**: Created dynamically by the Go service (example: `http://localhost:8080/`)
- **Stats Page**: No default HTTP `/stats` endpoint in `.devops/container/haproxy/haproxy.cfg`
- **Load Balancing**: Round-robin across 3 backends
- **Health Checks**: HTTP health checks on backends

## Quick Start

### 1. Start the Docker Compose Environment

```bash
make compose-up
```

This command builds the custom HAProxy image first (if needed). The image embeds `.devops/container/haproxy/haproxy.cfg`, and `make compose-up` initializes `./tmp/haproxy/haproxy.cfg` from that file when missing.

### 2. Verify Services are Running

```bash
make compose-ps
```

Should show all 4 services (3 backends + haproxy) as healthy.

### 3. Check HAProxy Admin Socket

```bash
make compose-stats
```

### 4. Test Load Balancing

```bash
# First create a frontend/backend configuration via gRPC (for example: make test-cases)
# Test round-robin load balancing
for i in {1..10}; do
  curl http://localhost:8080/ | head -20
  echo "---"
done
```

### 5. Connect Your Go Application

The Go load balancer manager should:

1. Set `HaproxyConfiguration.Socket.Address` to `./tmp/haproxy/master.sock` (local host run)
2. Ensure socket is mounted at runtime (Docker volume or host path)
3. Call `HaproxyService.GetStatus()` to query HAProxy stats
4. Expose results via gRPC

Example from config:

```yaml
haproxy:
  socket:
    network: "unix"
    address: "./tmp/haproxy/master.sock"
    timeout: "3s"
```

### 6. Test gRPC Query (if running Go app)

```bash
# Start the Go app (connects to HAProxy socket)
go run ./cmd/main.go

# In another terminal, query the gRPC service
grpcurl -plaintext localhost:50051 loadbalancer.v1.HaproxyStatusService.GetStatus
```

## Accessing HAProxy Sockets from Go App

### Option A: Docker Volume Mount (Recommended)

When running your Go app in Docker:

```yaml
volumes:
  - ./tmp/haproxy:/var/run/haproxy
```

### Option B: Host Path

When running your Go app on the host:

```bash
# Sockets are accessible in the project runtime directory after compose is running
ls -la ./tmp/haproxy/master.sock ./tmp/haproxy/admin.sock
```

### Option C: TCP Socket (Alternative)

Edit `haproxy.cfg` to expose admin socket via TCP:

```
stats socket 0.0.0.0:9001 level admin
```

## Configuration

### HAProxy Configuration File

See `haproxy.cfg` for details. Source of truth is `.devops/container/haproxy/haproxy.cfg`; local runtime copy is `./tmp/haproxy/haproxy.cfg`:

- **Global Settings**: Logging, socket configuration, performance tuning
- **Runtime Access**: Master socket (`/var/run/haproxy/master.sock`)
- **Admin/Health Access**: Admin socket (`/var/run/haproxy/admin.sock`)
- **Frontend**: HTTP entry point
- **Backend Pool**: Round-robin load balancing across 3 servers
- **Health Checks**: HTTP checks every 5 seconds

### Network Configuration

- **Internal Network**: `haproxy-internal` (bridge driver)
- **Host Network**: Allows HAProxy to communicate with localhost services
- **Volume Mount**: `./tmp/haproxy:/var/run/haproxy` for socket sharing

## Testing Scenarios

### 1. Backend Health Check

```bash
make compose-ps
test -S ./tmp/haproxy/admin.sock && echo "admin socket ready"
```

### 2. Restart a Backend

```bash
podman-compose -f .devops/compose/compose.yml restart backend-2
# HAProxy should mark it DOWN, then UP when it recovers
```

### 3. Scale Backends (Manual Edit Required)

Edit `compose.yml`, add more backend services, run:

```bash
make compose-up
```

### 4. Load Test

```bash
# Using Apache Bench
ab -n 1000 -c 10 http://localhost:8080/

# Using hey (Go)
go install github.com/rakyll/hey@latest
hey -n 1000 -c 10 http://localhost:8080/
```

## Troubleshooting

### Socket Permission Issues

If you get permission errors accessing the socket:

```bash
# Check socket exists and is readable
ls -la ./tmp/haproxy/master.sock ./tmp/haproxy/admin.sock

# Ensure volume is properly mounted
make compose-ps
```

### HAProxy Won't Start

```bash
# Check logs
make compose-logs

# Validate config syntax
podman run --rm go-loadbalancer-manager-haproxy:latest haproxy -f /usr/local/etc/haproxy/haproxy.cfg -c
```

### Backends Not Responding

```bash
# Check if backends are healthy
make compose-ps

# Check internal network connectivity
make compose-logs
```

### Socket Not Visible on Host

If running on Docker Desktop (Mac/Windows), sockets may not be accessible directly. Use:

- Bind mounts or named volumes mounted into the Go app container
- TCP socket configuration in HAProxy (port 9001)
- Docker Compose service name resolution

## Cleanup

```bash
# Stop and remove all services
make compose-down

# Remove volumes (optional)
make compose-clean

# View logs from stopped containers
make compose-logs
```

## Performance Tuning

For production-like testing:

1. Increase backend server count in `compose.yml`
2. Adjust HAProxy `maxconn` in `haproxy.cfg`
3. Tune timeout values for your use case
4. Add rate limiting rules if needed
5. Enable SSL/TLS (add certs to `certs/` directory)

## Next Steps

1. Configure your Go app to use `./tmp/haproxy/master.sock` (or `/var/run/haproxy/master.sock` in container)
2. Run `go run ./cmd/main.go` to connect and query stats
3. Verify gRPC endpoints expose HAProxy status correctly
4. Load test with various backend health scenarios
