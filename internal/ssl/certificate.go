package ssl

import (
	"crypto/tls"
	"crypto/x509"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

const (
	maxCertificatePEMSize = 256 << 10
	maxPrivateKeyPEMSize  = 64 << 10
)

type certificateMetadata struct {
	NotBefore time.Time
	NotAfter  time.Time
}

func validateCertificateMaterial(certPEM, keyPEM []byte, domain string, now time.Time) (certificateMetadata, error) {
	if len(certPEM) == 0 || len(keyPEM) == 0 {
		return certificateMetadata{}, model.NewValidationError("certificate and private key are required")
	}
	if len(certPEM) > maxCertificatePEMSize || len(keyPEM) > maxPrivateKeyPEMSize {
		return certificateMetadata{}, model.NewValidationError("certificate material is too large")
	}

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return certificateMetadata{}, model.NewValidationError("certificate or private key is invalid or does not match")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return certificateMetadata{}, model.NewValidationError("certificate is invalid")
	}
	if err := leaf.VerifyHostname(domain); err != nil {
		return certificateMetadata{}, model.NewValidationError("certificate does not cover the selected domain")
	}
	if leaf.NotBefore.After(now) {
		return certificateMetadata{}, model.NewValidationError("certificate is not valid yet")
	}
	if !leaf.NotAfter.After(now) {
		return certificateMetadata{}, model.NewValidationError("certificate has expired")
	}

	return certificateMetadata{NotBefore: leaf.NotBefore, NotAfter: leaf.NotAfter}, nil
}
