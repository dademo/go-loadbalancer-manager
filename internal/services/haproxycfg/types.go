// Package haproxycfg provides types and functions for rendering HAProxy configuration files.
package haproxycfg

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// HaproxyConfiguration defines one managed frontend/backend HAProxy configuration.
type HaproxyConfiguration struct {
	Name                string                   `json:"name"`
	FrontendName        string                   `json:"frontend_name"`
	FrontendBindAddress string                   `json:"frontend_bind_address"`
	FrontendBindPort    uint32                   `json:"frontend_bind_port"`
	URL                 string                   `json:"url"`
	LoadBalancing       string                   `json:"load_balancing_strategy"`
	BackendName         string                   `json:"backend_name"`
	Backends            []HaproxyBackendTarget   `json:"backends"`
	TrafficMode         string                   `json:"traffic_mode"`
	AutoHTTPSRedirect   bool                     `json:"auto_https_redirect"`
	TLS                 *HaproxyTLSConfiguration `json:"tls,omitempty"`
}

// HaproxyBackendTarget defines one backend server target.
type HaproxyBackendTarget struct {
	Name                string `json:"name"`
	Address             string `json:"address"`
	Port                uint32 `json:"port"`
	CheckIntervalSecond int64  `json:"check_interval_seconds"`
}

// HaproxyTLSConfiguration defines TLS options for frontend and backend handling.
type HaproxyTLSConfiguration struct {
	Enabled              bool   `json:"enabled"`
	CertificatePath      string `json:"certificate_path"`
	PrivateKeyPath       string `json:"private_key_path"`
	CertificatePEM       string `json:"certificate_pem"`
	PrivateKeyPEM        string `json:"private_key_pem"`
	SkipBackendTLSVerify bool   `json:"skip_backend_tls_verify"`
}

var (
	// ErrConfigurationNotFound indicates a requested managed configuration does not exist.
	ErrConfigurationNotFound = errors.New("haproxy configuration not found")
	// ErrConfigurationExists indicates a configuration with the same name already exists.
	ErrConfigurationExists = errors.New("haproxy configuration already exists")
	// ErrInvalidConfiguration indicates the provided configuration content is invalid.
	ErrInvalidConfiguration = errors.New("invalid haproxy configuration")
	// ErrInvalidConfigurationKey indicates the provided configuration key or name is invalid.
	ErrInvalidConfigurationKey = errors.New("invalid haproxy configuration key")
)

var allowedLoadBalancingStrategies = map[string]struct{}{
	"roundrobin": {},
	"leastconn":  {},
	"source":     {},
}

var allowedTrafficModes = map[string]struct{}{
	"http": {},
	"tcp":  {},
}

// NormalizeAndValidateConfigurationName normalizes and validates a configuration name.
func NormalizeAndValidateConfigurationName(name string) (string, error) {
	normalized := NormalizeConfigurationName(name)
	if normalized == "" {
		return "", fmt.Errorf("name is required: %w", ErrInvalidConfigurationKey)
	}

	return normalized, nil
}

// NormalizeConfigurationName normalizes a configuration name.
func NormalizeConfigurationName(name string) string {
	return strings.TrimSpace(name)
}

// NormalizeTrafficMode normalizes a traffic mode string.
func NormalizeTrafficMode(mode string) string {
	return strings.ToLower(strings.TrimSpace(mode))
}

// NormalizeLoadBalancingStrategy normalizes a load balancing strategy string.
func NormalizeLoadBalancingStrategy(strategy string) string {
	return strings.ToLower(strings.TrimSpace(strategy))
}

// ValidateConfiguration validates a HaproxyConfiguration.
func ValidateConfiguration(configuration HaproxyConfiguration) error {
	key, err := NormalizeAndValidateConfigurationName(configuration.Name)
	if err != nil {
		return err
	}
	if key == "" {
		return ErrInvalidConfigurationKey
	}

	if strings.TrimSpace(configuration.FrontendName) == "" {
		return fmt.Errorf("frontend_name is required: %w", ErrInvalidConfiguration)
	}

	if strings.TrimSpace(configuration.BackendName) == "" {
		return fmt.Errorf("backend_name is required: %w", ErrInvalidConfiguration)
	}

	trafficMode := NormalizeTrafficMode(configuration.TrafficMode)
	if trafficMode == "" {
		return fmt.Errorf("traffic_mode is required: %w", ErrInvalidConfiguration)
	}
	if _, ok := allowedTrafficModes[trafficMode]; !ok {
		return fmt.Errorf("traffic_mode must be one of http, tcp: %w", ErrInvalidConfiguration)
	}

	if trafficMode == "http" {
		if strings.TrimSpace(configuration.URL) == "" {
			return fmt.Errorf("url is required in http mode: %w", ErrInvalidConfiguration)
		}

		parsedURL, err := url.ParseRequestURI(strings.TrimSpace(configuration.URL))
		if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
			return fmt.Errorf("url must be a valid absolute URL: %w", ErrInvalidConfiguration)
		}
		if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			return fmt.Errorf("url must use http or https scheme in http mode: %w", ErrInvalidConfiguration)
		}
	}

	loadBalancing := strings.ToLower(strings.TrimSpace(configuration.LoadBalancing))
	if loadBalancing == "" {
		return fmt.Errorf("load_balancing_strategy is required: %w", ErrInvalidConfiguration)
	}
	if _, ok := allowedLoadBalancingStrategies[loadBalancing]; !ok {
		return fmt.Errorf("load_balancing_strategy must be one of roundrobin, leastconn, source: %w", ErrInvalidConfiguration)
	}

	if configuration.AutoHTTPSRedirect {
		if trafficMode != "http" {
			return fmt.Errorf("auto_https_redirect can only be enabled in http mode: %w", ErrInvalidConfiguration)
		}
		if configuration.TLS == nil || !configuration.TLS.Enabled {
			return fmt.Errorf("auto_https_redirect requires tls.enabled=true: %w", ErrInvalidConfiguration)
		}
	}

	if err := ValidateTLSConfiguration(configuration.TLS); err != nil {
		return err
	}

	if strings.TrimSpace(configuration.FrontendBindAddress) == "" {
		return fmt.Errorf("frontend_bind_address is required: %w", ErrInvalidConfiguration)
	}

	if configuration.FrontendBindPort == 0 || configuration.FrontendBindPort > 65535 {
		return fmt.Errorf("frontend_bind_port must be between 1 and 65535: %w", ErrInvalidConfiguration)
	}

	if len(configuration.Backends) == 0 {
		return fmt.Errorf("backends must contain at least one backend target: %w", ErrInvalidConfiguration)
	}

	backendNames := make(map[string]struct{}, len(configuration.Backends))
	for i, backend := range configuration.Backends {
		backendName := strings.TrimSpace(backend.Name)
		if backendName == "" {
			return fmt.Errorf("backends[%d].name is required: %w", i, ErrInvalidConfiguration)
		}

		normalizedBackendName := strings.ToLower(backendName)
		if _, exists := backendNames[normalizedBackendName]; exists {
			return fmt.Errorf("backends[%d].name duplicates an existing backend target: %w", i, ErrInvalidConfiguration)
		}
		backendNames[normalizedBackendName] = struct{}{}

		if strings.TrimSpace(backend.Address) == "" {
			return fmt.Errorf("backends[%d].address is required: %w", i, ErrInvalidConfiguration)
		}

		if backend.Port == 0 || backend.Port > 65535 {
			return fmt.Errorf("backends[%d].port must be between 1 and 65535: %w", i, ErrInvalidConfiguration)
		}

		if backend.CheckIntervalSecond <= 0 {
			return fmt.Errorf("backends[%d].check_interval_seconds must be > 0: %w", i, ErrInvalidConfiguration)
		}
	}

	return nil
}

// ValidateTLSConfiguration validates a HaproxyTLSConfiguration.
func ValidateTLSConfiguration(configuration *HaproxyTLSConfiguration) error {
	if configuration == nil {
		return nil
	}

	certificatePath := strings.TrimSpace(configuration.CertificatePath)
	privateKeyPath := strings.TrimSpace(configuration.PrivateKeyPath)
	certificatePEM := strings.TrimSpace(configuration.CertificatePEM)
	privateKeyPEM := strings.TrimSpace(configuration.PrivateKeyPEM)

	hasPathPair := certificatePath != "" || privateKeyPath != ""
	hasPEMPair := certificatePEM != "" || privateKeyPEM != ""

	if !configuration.Enabled {
		if hasPathPair || hasPEMPair {
			return fmt.Errorf("tls certificates cannot be provided when tls.enabled=false: %w", ErrInvalidConfiguration)
		}
		return nil
	}

	if hasPathPair {
		if certificatePath == "" || privateKeyPath == "" {
			return fmt.Errorf("tls.certificate_path and tls.private_key_path must both be provided: %w", ErrInvalidConfiguration)
		}
	}

	if hasPEMPair {
		if certificatePEM == "" || privateKeyPEM == "" {
			return fmt.Errorf("tls.certificate_pem and tls.private_key_pem must both be provided: %w", ErrInvalidConfiguration)
		}

		if err := ValidateCertificatePEM(certificatePEM); err != nil {
			return err
		}

		if err := ValidatePrivateKeyPEM(privateKeyPEM); err != nil {
			return err
		}
	}

	if hasPathPair && hasPEMPair {
		return fmt.Errorf("provide either tls path pair or tls pem pair, not both: %w", ErrInvalidConfiguration)
	}

	if !hasPathPair && !hasPEMPair {
		return fmt.Errorf("tls.enabled=true requires certificate/key configuration: %w", ErrInvalidConfiguration)
	}

	return nil
}

// CloneConfiguration returns a deep copy of a HaproxyConfiguration.
func CloneConfiguration(configuration HaproxyConfiguration) HaproxyConfiguration {
	cloned := configuration
	if len(configuration.Backends) > 0 {
		cloned.Backends = append([]HaproxyBackendTarget(nil), configuration.Backends...)
	}
	if configuration.TLS != nil {
		tls := *configuration.TLS
		cloned.TLS = &tls
	}

	return cloned
}
