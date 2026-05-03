package services

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	loadbalancerv1 "dademo.fr/loadbalancer-manager/internal/gen/proto/loadbalancer/v1"
	"github.com/rs/zerolog"
)

// NetworkSocketsService provides information about network sockets and their states.
type NetworkSocketsService struct {
	logger      zerolog.Logger
	processName string // Name of the process to monitor (e.g., "haproxy")
}

// newNetworkSocketsService creates a new network sockets service.
func newNetworkSocketsService(logger zerolog.Logger) *NetworkSocketsService {
	return &NetworkSocketsService{
		logger:      logger.With().Str("component", "network_sockets_service").Logger(),
		processName: "haproxy", // Default to haproxy, but can be overridden
	}
}

// SocketStateMap maps the numeric state from /proc/net/tcp to the proto SocketState.
var socketStateMap = map[int]loadbalancerv1.SocketState{
	0x01: loadbalancerv1.SocketState_SOCKET_STATE_ESTABLISHED,
	0x02: loadbalancerv1.SocketState_SOCKET_STATE_SYN_SENT,
	0x03: loadbalancerv1.SocketState_SOCKET_STATE_SYN_RECV,
	0x04: loadbalancerv1.SocketState_SOCKET_STATE_FIN_WAIT1,
	0x05: loadbalancerv1.SocketState_SOCKET_STATE_FIN_WAIT2,
	0x06: loadbalancerv1.SocketState_SOCKET_STATE_TIME_WAIT,
	0x07: loadbalancerv1.SocketState_SOCKET_STATE_CLOSED,
	0x08: loadbalancerv1.SocketState_SOCKET_STATE_CLOSE_WAIT,
	0x09: loadbalancerv1.SocketState_SOCKET_STATE_LAST_ACK,
	0x0A: loadbalancerv1.SocketState_SOCKET_STATE_LISTEN,
	0x0B: loadbalancerv1.SocketState_SOCKET_STATE_CLOSING,
}

// GetNetworkSockets retrieves all network socket connections from /proc/net/tcp and /proc/net/tcp6
// that belong to the current service process.
func (s *NetworkSocketsService) GetNetworkSockets(ctx context.Context) (*loadbalancerv1.GetNetworkSocketsResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Get inodes for current process
	serviceInodes, err := s.getProcessInodes()
	if err != nil {
		s.logger.Warn().Err(err).Msg("Failed to get process inodes, returning all sockets")
		// Continue anyway, but we won't filter
		serviceInodes = make(map[uint32]bool)
	}

	connections := make([]*loadbalancerv1.SocketConnection, 0)
	socketsByState := make(map[loadbalancerv1.SocketState][]*loadbalancerv1.SocketConnection)

	// Read IPv4 sockets
	ipv4Conns, err := s.readSockets("/proc/net/tcp", "IPv4", serviceInodes)
	if err != nil {
		s.logger.Warn().Err(err).Msg("Failed to read IPv4 sockets")
	} else {
		connections = append(connections, ipv4Conns...)
	}

	// Read IPv6 sockets
	ipv6Conns, err := s.readSockets("/proc/net/tcp6", "IPv6", serviceInodes)
	if err != nil {
		s.logger.Warn().Err(err).Msg("Failed to read IPv6 sockets")
	} else {
		connections = append(connections, ipv6Conns...)
	}

	// Group sockets by state and count specific states
	establishedCount := 0
	listenCount := 0
	timeWaitCount := 0

	for _, conn := range connections {
		socketsByState[conn.State] = append(socketsByState[conn.State], conn)
		switch conn.State {
		case loadbalancerv1.SocketState_SOCKET_STATE_ESTABLISHED:
			establishedCount++
		case loadbalancerv1.SocketState_SOCKET_STATE_LISTEN:
			listenCount++
		case loadbalancerv1.SocketState_SOCKET_STATE_TIME_WAIT:
			timeWaitCount++
		}
	}

	// Convert map to sorted list of SocketsByState
	socketsByStateList := make([]*loadbalancerv1.SocketsByState, 0, len(socketsByState))
	for state, conns := range socketsByState {
		socketsByStateList = append(socketsByStateList, &loadbalancerv1.SocketsByState{
			State:       state,
			Connections: conns,
			Count:       int32(len(conns)),
		})
	}

	response := &loadbalancerv1.GetNetworkSocketsResponse{
		SocketsByState:   socketsByStateList,
		TotalSockets:     int32(len(connections)),
		EstablishedCount: int32(establishedCount),
		ListenCount:      int32(listenCount),
		TimeWaitCount:    int32(timeWaitCount),
	}

	return response, nil
}

// getProcessInodes retrieves the inodes of all socket file descriptors for all processes with the configured name (including forked children).
func (s *NetworkSocketsService) getProcessInodes() (map[uint32]bool, error) {
	inodes := make(map[uint32]bool)

	// Find all process PIDs (including forked children)
	processPIDs, err := s.findAllProcessPIDs()
	if err != nil {
		return inodes, err
	}

	if len(processPIDs) == 0 {
		return inodes, fmt.Errorf("no %s processes found", s.processName)
	}

	s.logger.Debug().Ints("pids", processPIDs).Str("process_name", s.processName).Msg("Found processes")

	// Collect inodes from all processes
	for _, pid := range processPIDs {
		fdDir := filepath.Join("/proc", strconv.Itoa(pid), "fd")

		entries, err := os.ReadDir(fdDir)
		if err != nil {
			s.logger.Debug().Int("pid", pid).Err(err).Msg("Failed to read fd directory")
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				// Try to read the symlink target
				target, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
				if err != nil {
					continue
				}

				// Extract inode from socket:[inode] format
				if strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
					inodeStr := strings.TrimPrefix(strings.TrimSuffix(target, "]"), "socket:[")
					inode, err := strconv.ParseUint(inodeStr, 10, 32)
					if err == nil {
						inodes[uint32(inode)] = true
					}
				}
			}
		}
	}

	if len(inodes) == 0 {
		s.logger.Warn().Str("process_name", s.processName).Msg("No socket inodes found for processes")
	}

	return inodes, nil
}

// findAllProcessPIDs finds all PIDs of processes with the specified name (including forked children).
func (s *NetworkSocketsService) findAllProcessPIDs() ([]int, error) {
	var processPIDs []int
	procDir := "/proc"

	entries, err := os.ReadDir(procDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pidStr := entry.Name()
		// Only process numeric directories
		if _, err := strconv.Atoi(pidStr); err != nil {
			continue
		}

		// Read the comm file to get the process name
		commPath := filepath.Join(procDir, pidStr, "comm")
		commBytes, err := os.ReadFile(commPath)
		if err != nil {
			continue
		}

		commName := strings.TrimSpace(string(commBytes))
		if strings.Contains(commName, s.processName) {
			pid, _ := strconv.Atoi(pidStr)
			processPIDs = append(processPIDs, pid)
		}
	}

	// If no processes found, return error
	if len(processPIDs) == 0 {
		return nil, fmt.Errorf("%s process not found in /proc", s.processName)
	}

	return processPIDs, nil
}

// readSockets reads socket information from /proc/net/tcp or /proc/net/tcp6.
// It filters the results to only include sockets owned by the service process.
func (s *NetworkSocketsService) readSockets(filePath string, protocol string, serviceInodes map[uint32]bool) ([]*loadbalancerv1.SocketConnection, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	connections := make([]*loadbalancerv1.SocketConnection, 0)
	scanner := bufio.NewScanner(file)

	// Skip header line
	if !scanner.Scan() {
		return connections, nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)

		// /proc/net/tcp format:
		// sl local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
		if len(fields) < 10 {
			continue
		}

		localAddr := fields[1]
		remoteAddr := fields[2]
		state := fields[3]

		uid, err := strconv.ParseUint(fields[7], 10, 32)
		if err != nil {
			continue
		}

		inode, err := strconv.ParseUint(fields[9], 10, 32)
		if err != nil {
			continue
		}

		// Filter to only include sockets from the service process
		if len(serviceInodes) > 0 && !serviceInodes[uint32(inode)] {
			continue
		}

		localIP, localPort, err := parseAddress(localAddr, protocol)
		if err != nil {
			continue
		}

		remoteIP, remotePort, err := parseAddress(remoteAddr, protocol)
		if err != nil {
			continue
		}

		stateInt, err := strconv.ParseInt(state, 16, 32)
		if err != nil {
			continue
		}

		socketState, ok := socketStateMap[int(stateInt)]
		if !ok {
			socketState = loadbalancerv1.SocketState_SOCKET_STATE_UNSPECIFIED
		}

		conn := &loadbalancerv1.SocketConnection{
			LocalAddress:  localIP,
			LocalPort:     uint32(localPort),
			RemoteAddress: remoteIP,
			RemotePort:    uint32(remotePort),
			State:         socketState,
			Uid:           uint32(uid),
			Inode:         uint32(inode),
		}

		connections = append(connections, conn)
	}

	return connections, scanner.Err()
}

// parseAddress parses the address and port from /proc/net/tcp format.
// Format for IPv4: AABBCCDD:EEEE (hexadecimal)
// Format for IPv6: 00000000000000000000000000000001:EEEE (hexadecimal)
func parseAddress(addrStr string, protocol string) (string, int, error) {
	parts := strings.Split(addrStr, ":")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid address format: %s", addrStr)
	}

	addrHex := parts[0]
	portHex := parts[1]

	port, err := strconv.ParseInt(portHex, 16, 32)
	if err != nil {
		return "", 0, err
	}

	var ip string
	if protocol == "IPv4" {
		// IPv4: reverse byte order
		if len(addrHex) != 8 {
			return "", 0, fmt.Errorf("invalid IPv4 address: %s", addrHex)
		}
		b1, _ := strconv.ParseInt(addrHex[6:8], 16, 32)
		b2, _ := strconv.ParseInt(addrHex[4:6], 16, 32)
		b3, _ := strconv.ParseInt(addrHex[2:4], 16, 32)
		b4, _ := strconv.ParseInt(addrHex[0:2], 16, 32)
		ip = fmt.Sprintf("%d.%d.%d.%d", b1, b2, b3, b4)
	} else if protocol == "IPv6" {
		// IPv6: convert from hex to standard format
		ip = parseIPv6(addrHex)
	} else {
		return "", 0, fmt.Errorf("unsupported protocol: %s", protocol)
	}

	return ip, int(port), nil
}

// parseIPv6 converts the IPv6 address from /proc/net/tcp6 format to standard notation.
func parseIPv6(addrHex string) string {
	if len(addrHex) != 32 {
		return addrHex // Return as-is if format is unexpected
	}

	var parts []string
	for i := 0; i < 32; i += 8 {
		chunk := addrHex[i : i+8]
		// Convert to uint32 and reverse byte order
		val, _ := strconv.ParseUint(chunk, 16, 32)

		b1 := (val >> 24) & 0xFF
		b2 := (val >> 16) & 0xFF
		b3 := (val >> 8) & 0xFF
		b4 := val & 0xFF

		part := fmt.Sprintf("%02x%02x:%02x%02x", b4, b3, b2, b1)
		parts = append(parts, part)
	}

	// Compress IPv6 address (remove leading zeros)
	ip := strings.Join(parts, ":")
	return compressIPv6(ip)
}

// compressIPv6 compresses IPv6 address by removing leading zeros.
func compressIPv6(ip string) string {
	// Simple compression: just remove leading zeros from each part
	parts := strings.Split(ip, ":")
	for i, part := range parts {
		parts[i] = strings.TrimLeft(part, "0")
		if parts[i] == "" {
			parts[i] = "0"
		}
	}
	return strings.Join(parts, ":")
}
