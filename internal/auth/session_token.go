package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// Session tokens are the bearer credential in the session cookie, so they
// are generated with crypto/rand and only their SHA-256 is stored: a leaked
// database row (or the session list) cannot be replayed as a session.

// newSessionToken returns a 256-bit URL-safe random token.
func newSessionToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// hashSessionToken returns the stored form of a session token.
func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
