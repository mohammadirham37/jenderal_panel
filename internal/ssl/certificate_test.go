package ssl

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

func testCertificate(t *testing.T, dnsNames []string, notBefore, notAfter time.Time) ([]byte, []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		DNSNames:     dnsNames,
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func TestValidateCertificateMaterial(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	validUntil := now.Add(30 * 24 * time.Hour)
	validCert, validKey := testCertificate(t, []string{"example.com"}, now.Add(-time.Hour), validUntil)

	t.Run("valid exact domain", func(t *testing.T) {
		metadata, err := validateCertificateMaterial(validCert, validKey, "example.com", now)
		if err != nil {
			t.Fatalf("validateCertificateMaterial() error = %v", err)
		}
		if !metadata.NotAfter.Equal(validUntil) {
			t.Fatalf("NotAfter = %v, want %v", metadata.NotAfter, validUntil)
		}
	})

	t.Run("valid wildcard", func(t *testing.T) {
		certPEM, keyPEM := testCertificate(t, []string{"*.example.com"}, now.Add(-time.Hour), validUntil)
		if _, err := validateCertificateMaterial(certPEM, keyPEM, "www.example.com", now); err != nil {
			t.Fatalf("validateCertificateMaterial() error = %v", err)
		}
	})

	tests := []struct {
		name    string
		domain  string
		certPEM []byte
		keyPEM  []byte
	}{
		{name: "malformed certificate", domain: "example.com", certPEM: []byte("not pem"), keyPEM: validKey},
		{name: "wrong hostname", domain: "other.example.com", certPEM: validCert, keyPEM: validKey},
		{name: "certificate too large", domain: "example.com", certPEM: []byte(strings.Repeat("x", (256<<10)+1)), keyPEM: validKey},
		{name: "key too large", domain: "example.com", certPEM: validCert, keyPEM: []byte(strings.Repeat("x", (64<<10)+1))},
	}

	otherCert, otherKey := testCertificate(t, []string{"example.com"}, now.Add(-time.Hour), validUntil)
	_ = otherCert
	tests = append(tests, struct {
		name    string
		domain  string
		certPEM []byte
		keyPEM  []byte
	}{name: "mismatched key", domain: "example.com", certPEM: validCert, keyPEM: otherKey})

	futureCert, futureKey := testCertificate(t, []string{"example.com"}, now.Add(time.Hour), validUntil)
	tests = append(tests, struct {
		name    string
		domain  string
		certPEM []byte
		keyPEM  []byte
	}{name: "not valid yet", domain: "example.com", certPEM: futureCert, keyPEM: futureKey})

	expiredCert, expiredKey := testCertificate(t, []string{"example.com"}, now.Add(-48*time.Hour), now.Add(-time.Hour))
	tests = append(tests, struct {
		name    string
		domain  string
		certPEM []byte
		keyPEM  []byte
	}{name: "expired", domain: "example.com", certPEM: expiredCert, keyPEM: expiredKey})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := validateCertificateMaterial(tt.certPEM, tt.keyPEM, tt.domain, now); err == nil {
				t.Fatal("validateCertificateMaterial() error = nil, want validation error")
			}
		})
	}
}
