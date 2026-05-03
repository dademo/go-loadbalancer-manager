package haproxycfg

import (
	"fmt"
	"strings"
)

// RenderFrontendSection renders the HAProxy frontend stanza for the given configuration.
func RenderFrontendSection(configuration HaproxyConfiguration) ([]string, error) {
	trafficMode := NormalizeTrafficMode(configuration.TrafficMode)

	bindLine := fmt.Sprintf("    bind %s:%d", strings.TrimSpace(configuration.FrontendBindAddress), configuration.FrontendBindPort)
	if configuration.TLS != nil && configuration.TLS.Enabled {
		certificatePath := strings.TrimSpace(configuration.TLS.CertificatePath)
		if certificatePath == "" {
			return nil, fmt.Errorf("tls.enabled=true requires tls.certificate_path when rendering HAProxy config: %w", ErrInvalidConfiguration)
		}
		bindLine = fmt.Sprintf("%s ssl crt %s", bindLine, certificatePath)
	}

	lines := []string{
		fmt.Sprintf("frontend %s", configuration.FrontendName),
		bindLine,
		fmt.Sprintf("    mode %s", trafficMode),
		fmt.Sprintf("    default_backend %s", configuration.BackendName),
	}
	if configuration.AutoHTTPSRedirect {
		lines = append(lines, "    http-request redirect scheme https code 301 if !{ ssl_fc }")
	}

	return lines, nil
}
