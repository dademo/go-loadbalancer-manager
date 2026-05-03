package haproxycfg

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ComputeCertificatesDirectory returns the path to the certificates directory,
// which is a "certificates" subdirectory next to the HAProxy configuration file.
func ComputeCertificatesDirectory(configurationFile string) string {
	return filepath.Join(filepath.Dir(configurationFile), "certificates")
}

// EnsureCertificatePath ensures the TLS configuration has a usable certificate file path.
// If a path is already set it is left unchanged. If PEM data is provided instead, the
// combined certificate+key is written to certificatesDir and the path fields are updated.
func EnsureCertificatePath(configuration *HaproxyConfiguration, certificatesDir string) error {
	if configuration.TLS == nil || !configuration.TLS.Enabled {
		return nil
	}

	certificatePath := strings.TrimSpace(configuration.TLS.CertificatePath)
	certificatePEM := strings.TrimSpace(configuration.TLS.CertificatePEM)
	privateKeyPEM := strings.TrimSpace(configuration.TLS.PrivateKeyPEM)

	if certificatePath != "" {
		return nil
	}

	if certificatePEM != "" && privateKeyPEM != "" {
		fileName := fmt.Sprintf("%s.pem", strings.ToLower(strings.ReplaceAll(configuration.Name, " ", "_")))
		filePath := filepath.Join(certificatesDir, fileName)

		// HAProxy expects certificate and key combined in a single PEM file.
		combinedPEM := certificatePEM + "\n" + privateKeyPEM
		if err := os.WriteFile(filePath, []byte(combinedPEM), 0600); err != nil {
			return fmt.Errorf("unable to write certificate file %q: %w", filePath, err)
		}

		configuration.TLS.CertificatePath = filePath
		configuration.TLS.CertificatePEM = ""
		configuration.TLS.PrivateKeyPEM = ""
	}

	return nil
}

// ValidateCertificatePEM validates that the provided PEM data is a well-formed certificate.
// It does not check expiration date.
func ValidateCertificatePEM(certPEM string) error {
	if strings.TrimSpace(certPEM) == "" {
		return fmt.Errorf("certificate PEM data is empty: %w", ErrInvalidConfiguration)
	}

	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return fmt.Errorf("invalid certificate PEM format: could not parse PEM block: %w", ErrInvalidConfiguration)
	}

	if block.Type != "CERTIFICATE" {
		return fmt.Errorf("invalid certificate PEM: expected CERTIFICATE block, got %s: %w", block.Type, ErrInvalidConfiguration)
	}

	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return fmt.Errorf("invalid certificate PEM: %w", ErrInvalidConfiguration)
	}

	return nil
}

// ValidatePrivateKeyPEM validates that the provided PEM data is a well-formed private key.
func ValidatePrivateKeyPEM(keyPEM string) error {
	if strings.TrimSpace(keyPEM) == "" {
		return fmt.Errorf("private key PEM data is empty: %w", ErrInvalidConfiguration)
	}

	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		return fmt.Errorf("invalid private key PEM format: could not parse PEM block: %w", ErrInvalidConfiguration)
	}

	keyTypes := map[string]bool{
		"RSA PRIVATE KEY":     true,
		"PRIVATE KEY":         true,
		"EC PRIVATE KEY":      true,
		"DSA PRIVATE KEY":     true,
		"OPENSSH PRIVATE KEY": true,
	}

	if !keyTypes[block.Type] {
		return fmt.Errorf("invalid private key PEM: expected private key block, got %s: %w", block.Type, ErrInvalidConfiguration)
	}

	_, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		_, rsaErr := x509.ParsePKCS1PrivateKey(block.Bytes)
		if rsaErr != nil {
			_, ecErr := x509.ParseECPrivateKey(block.Bytes)
			if ecErr != nil {
				return fmt.Errorf("invalid private key PEM: could not parse as PKCS8, PKCS1, or EC key: %w", ErrInvalidConfiguration)
			}
		}
	}

	return nil
}
