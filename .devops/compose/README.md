# HAProxy + Backends Docker Compose Setup

This docker-compose configuration creates a complete test environment for the Go load balancer manager with HAProxy.

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│ Host Machine (localhost)                                │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │ Go Load Balancer Manager (cmd/main.go)            │ │
│  │ - Connects to: /var/run/haproxy/admin.sock        │ │
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
│  │              │ admin.sock       │ ←─────────────┐   │ │
│  │              │ :80 (stats)      │               │   │ │
│  │              └──────────────────┘               │   │ │
│  │                                                  │   │ │
│  └──────────────────────────────────────────────────┼───┘ │
│           ↓ ports: 8080 (localhost)     │            │
│                                         │            │
│    ┌──────────────────────────────┐    │            │
│    │ Host Network Bridge          │────┘            │
│    │ (volume mount admin.sock)    │                 │
│    └──────────────────────────────┘                 │
│                                                      │
│    Socket available at:                             │
│    /var/run/haproxy/admin.sock                      │
└─────────────────────────────────────────────────────────┘
```

## Services

### Backends (backend-1, backend-2, backend-3)

- **Image**: `python:3.11-alpine`
- **Ports**: 8001, 8002, 8003 (on internal network)
- **Function**: Simple HTTP servers for load balancing testing
- **Health**: HTTP health checks every 5s

### HAProxy

- **Image**: `haproxy:2.8-alpine`
- **Networks**: Internal (`haproxy-internal`) + Host
- **Exposed Ports**:
  - `8080`: HTTP stats page (localhost:8080/stats)
  - `8443`: HTTPS (if certificates added)
  - `50051`: gRPC (if needed)
- **Admin Socket**: `/var/run/haproxy/admin.sock`
- **Load Balancing**: Round-robin across 3 backends
- **Health Checks**: HTTP health checks on backends

## Quick Start

### 1. Start the Docker Compose Environment

```bash
cd .devops/compose
docker-compose up -d
```

### 2. Verify Services are Running

```bash
docker-compose ps
```

Should show all 4 services (3 backends + haproxy) as healthy.

### 3. Check HAProxy Stats

Open in browser or curl:

```bash
curl http://localhost:8080/stats
```

### 4. Test Load Balancing

```bash
# Test round-robin load balancing
for i in {1..10}; do
  curl http://localhost:8080/ | head -20
  echo "---"
done
```

### 5. Connect Your Go Application

The Go load balancer manager should:

1. Set `HaproxyConfiguration.Socket.Address` to `/var/run/haproxy/admin.sock`
2. Ensure socket is mounted at runtime (Docker volume or host path)
3. Call `HaproxyService.GetStatus()` to query HAProxy stats
4. Expose results via gRPC

Example from config:

```yaml
haproxy:
  socket:
    network: "unix"
    address: "/var/run/haproxy/admin.sock"
    timeout: "3s"
```

### 6. Test gRPC Query (if running Go app)

```bash
# Start the Go app (connects to HAProxy socket)
go run ./cmd/main.go

# In another terminal, query the gRPC service
grpcurl -plaintext localhost:50051 loadbalancer.v1.HaproxyStatusService.GetStatus
```

## Accessing HAProxy Admin Socket from Go App

### Option A: Docker Volume Mount (Recommended)

When running your Go app in Docker:

```yaml
volumes:
  - haproxy-socket:/var/run/haproxy
```

### Option B: Host Path

When running your Go app on the host:

```bash
# The socket is accessible at the host path after compose is running
ls -la /var/run/haproxy/admin.sock
```

### Option C: TCP Socket (Alternative)

Edit `haproxy.cfg` to expose stats via TCP:

```
stats socket 0.0.0.0:9001 level admin
```

## Configuration

### HAProxy Configuration File

See `haproxy.cfg` for details:

- **Global Settings**: Logging, socket configuration, performance tuning
- **Stats Page**: HTTP interface on port 80 (`/stats`)
- **Frontend**: HTTP entry point
- **Backend Pool**: Round-robin load balancing across 3 servers
- **Health Checks**: HTTP checks every 5 seconds

### Network Configuration

- **Internal Network**: `haproxy-internal` (bridge driver)
- **Host Network**: Allows HAProxy to communicate with localhost services
- **Volume**: `haproxy-socket` for admin socket sharing

## Testing Scenarios

### 1. Backend Health Check

```bash
docker-compose exec haproxy cat /var/run/haproxy/admin.sock
```

### 2. Restart a Backend

```bash
docker-compose restart backend-2
# HAProxy should mark it DOWN, then UP when it recovers
```

### 3. Scale Backends (Manual Edit Required)

Edit `docker-compose.yml`, add more backend services, run:

```bash
docker-compose up -d
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
ls -la /var/run/haproxy/admin.sock

# Ensure volume is properly mounted
docker-compose exec haproxy ls -la /var/run/haproxy/admin.sock
```

### HAProxy Won't Start

```bash
# Check logs
docker-compose logs haproxy

# Validate config syntax
docker run --rm -v $(pwd)/haproxy.cfg:/haproxy.cfg haproxy:2.8-alpine \
  haproxy -f /haproxy.cfg -c
```

### Backends Not Responding

```bash
# Check if backends are healthy
docker-compose exec haproxy wget -qO- http://backend-1:8001

# Check internal network connectivity
docker-compose exec backend-1 ping backend-2
```

### Socket Not Visible on Host

If running on Docker Desktop (Mac/Windows), sockets may not be accessible directly. Use:

- Docker volumes (mounted into Go app container)
- TCP socket configuration in HAProxy (port 9001)
- Docker Compose service name resolution

## Cleanup

```bash
# Stop and remove all services
docker-compose down

# Remove volumes (optional)
docker-compose down -v

# View logs from stopped containers
docker-compose logs
```

## Performance Tuning

For production-like testing:

1. Increase backend server count in `docker-compose.yml`
2. Adjust HAProxy `maxconn` in `haproxy.cfg`
3. Tune timeout values for your use case
4. Add rate limiting rules if needed
5. Enable SSL/TLS (add certs to `certs/` directory)

## Next Steps

1. Configure your Go app to use `/var/run/haproxy/admin.sock`
2. Run `go run ./cmd/main.go` to connect and query stats
3. Verify gRPC endpoints expose HAProxy status correctly
4. Load test with various backend health scenarios
