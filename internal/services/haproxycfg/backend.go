package haproxycfg

import (
	"fmt"
	"strings"
)

// RenderBackendSection renders the HAProxy backend stanza for the given configuration.
func RenderBackendSection(configuration HaproxyConfiguration) []string {
	trafficMode := NormalizeTrafficMode(configuration.TrafficMode)
	loadBalancing := NormalizeLoadBalancingStrategy(configuration.LoadBalancing)

	lines := make([]string, 0, 3+len(configuration.Backends))
	lines = append(lines,
		fmt.Sprintf("backend %s", configuration.BackendName),
		fmt.Sprintf("    mode %s", trafficMode),
		fmt.Sprintf("    balance %s", loadBalancing),
	)

	for _, backend := range configuration.Backends {
		serverLine := fmt.Sprintf(
			"    server %s %s:%d check inter %ds",
			backend.Name,
			strings.TrimSpace(backend.Address),
			backend.Port,
			backend.CheckIntervalSecond,
		)
		if configuration.TLS != nil && configuration.TLS.SkipBackendTLSVerify {
			serverLine += " ssl verify none"
		}
		lines = append(lines, serverLine)
	}

	return lines
}
