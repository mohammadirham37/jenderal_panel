package ssl

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/http/webroot"
	"github.com/go-acme/lego/v4/registration"
)

// ACMEClient abstracts certificate issuance and revocation so that tests
// can substitute a mock without touching any real ACME server.
type ACMEClient interface {
	ObtainCertificate(domain string, webroot string) (certPEM, keyPEM []byte, err error)
	RevokeCertificate(certPEM []byte) error
}

// ----------------------------------------------------------------
// LegoClient — production implementation using go-acme/lego
// ----------------------------------------------------------------

// LegoClient implements ACMEClient using the lego ACME library.
// By default it targets Let's Encrypt production. LEGO_CA_URL may override it,
// for example with the Let's Encrypt staging directory during testing.
type LegoClient struct {
	email      string
	accountDir string
}

// NewLegoClient creates a new LegoClient.
// accountDir is the directory where the ACME account key and registration are persisted.
func NewLegoClient(email, accountDir string) *LegoClient {
	return &LegoClient{email: email, accountDir: accountDir}
}

func newHTTP01Provider(root string) (challenge.Provider, error) {
	return webroot.NewHTTPProvider(root)
}

func acmeDirectoryURL() string {
	if url := os.Getenv("LEGO_CA_URL"); url != "" {
		return url
	}
	return lego.LEDirectoryProduction
}

// legoUser implements the registration.User interface required by lego.
type legoUser struct {
	Email        string                 `json:"email"`
	Registration *registration.Resource `json:"registration,omitempty"`
	key          crypto.PrivateKey
}

func (u *legoUser) GetEmail() string                        { return u.Email }
func (u *legoUser) GetRegistration() *registration.Resource { return u.Registration }
func (u *legoUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

// loadOrCreateAccount loads an existing ACME account from disk, or generates
// a new one and registers it with the CA.
func (c *LegoClient) loadOrCreateAccount() (*legoUser, error) {
	keyPath := filepath.Join(c.accountDir, "account.key")
	regPath := filepath.Join(c.accountDir, "registration.json")

	if err := os.MkdirAll(c.accountDir, 0700); err != nil {
		return nil, fmt.Errorf("create account dir: %w", err)
	}

	user := &legoUser{Email: c.email}

	// Try loading existing key.
	keyPEM, err := os.ReadFile(keyPath)
	if err == nil {
		block, _ := pem.Decode(keyPEM)
		if block != nil {
			privKey, parseErr := x509.ParseECPrivateKey(block.Bytes)
			if parseErr != nil {
				return nil, fmt.Errorf("parse account key: %w", parseErr)
			}
			user.key = privKey
		}
	}

	// Generate a new key if none was loaded.
	if user.key == nil {
		privKey, genErr := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if genErr != nil {
			return nil, fmt.Errorf("generate account key: %w", genErr)
		}
		user.key = privKey

		derBytes, marshalErr := x509.MarshalECPrivateKey(privKey)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal account key: %w", marshalErr)
		}
		pemBlock := &pem.Block{Type: "EC PRIVATE KEY", Bytes: derBytes}
		if writeErr := os.WriteFile(keyPath, pem.EncodeToMemory(pemBlock), 0600); writeErr != nil {
			return nil, fmt.Errorf("write account key: %w", writeErr)
		}
	}

	// Try loading existing registration.
	regData, err := os.ReadFile(regPath)
	if err == nil {
		var reg registration.Resource
		if jsonErr := json.Unmarshal(regData, &reg); jsonErr == nil {
			user.Registration = &reg
		}
	}

	return user, nil
}

// ObtainCertificate requests a new certificate from the ACME CA using the
// HTTP-01 challenge with a webroot provider.
func (c *LegoClient) ObtainCertificate(domain string, webroot string) ([]byte, []byte, error) {
	user, err := c.loadOrCreateAccount()
	if err != nil {
		return nil, nil, err
	}

	config := lego.NewConfig(user)
	config.Certificate.KeyType = certcrypto.RSA2048

	config.CADirURL = acmeDirectoryURL()

	client, err := lego.NewClient(config)
	if err != nil {
		return nil, nil, fmt.Errorf("create lego client: %w", err)
	}

	// Let Nginx keep port 80 and serve the challenge file from the website root.
	provider, err := newHTTP01Provider(webroot)
	if err != nil {
		return nil, nil, fmt.Errorf("create HTTP-01 webroot provider: %w", err)
	}
	if err := client.Challenge.SetHTTP01Provider(provider); err != nil {
		return nil, nil, fmt.Errorf("set http01 provider: %w", err)
	}

	// Register if we don't have a registration yet.
	if user.Registration == nil {
		reg, regErr := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if regErr != nil {
			return nil, nil, fmt.Errorf("register ACME account: %w", regErr)
		}
		user.Registration = reg

		// Persist registration.
		regData, _ := json.MarshalIndent(reg, "", "  ")
		regPath := filepath.Join(c.accountDir, "registration.json")
		_ = os.WriteFile(regPath, regData, 0600)
	}

	request := certificate.ObtainRequest{
		Domains: []string{domain},
		Bundle:  true,
	}
	cert, err := client.Certificate.Obtain(request)
	if err != nil {
		return nil, nil, fmt.Errorf("obtain certificate: %w", err)
	}

	return cert.Certificate, cert.PrivateKey, nil
}

// RevokeCertificate revokes a previously issued certificate.
func (c *LegoClient) RevokeCertificate(certPEM []byte) error {
	user, err := c.loadOrCreateAccount()
	if err != nil {
		return err
	}

	config := lego.NewConfig(user)
	config.Certificate.KeyType = certcrypto.RSA2048

	config.CADirURL = acmeDirectoryURL()

	client, err := lego.NewClient(config)
	if err != nil {
		return fmt.Errorf("create lego client: %w", err)
	}

	if user.Registration == nil {
		reg, regErr := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if regErr != nil {
			return fmt.Errorf("register ACME account: %w", regErr)
		}
		user.Registration = reg
	}

	if err := client.Certificate.Revoke(certPEM); err != nil {
		return fmt.Errorf("revoke certificate: %w", err)
	}

	return nil
}

// ----------------------------------------------------------------
// MockACMEClient — test double
// ----------------------------------------------------------------

// MockACMEClient is a test double for ACMEClient.
type MockACMEClient struct {
	ObtainFunc func(domain, webroot string) ([]byte, []byte, error)
	RevokeFunc func(certPEM []byte) error
}

// ObtainCertificate delegates to ObtainFunc.
func (m *MockACMEClient) ObtainCertificate(domain string, webroot string) ([]byte, []byte, error) {
	return m.ObtainFunc(domain, webroot)
}

// RevokeCertificate delegates to RevokeFunc.
func (m *MockACMEClient) RevokeCertificate(certPEM []byte) error {
	return m.RevokeFunc(certPEM)
}
