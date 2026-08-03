// Package signing creates webhook signing secrets and (from phase 3) signs payloads.
package signing

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

const (
	// SecretPrefix makes secrets recognisable in logs and secret scanners.
	SecretPrefix = "whsec_"
	secretBytes  = 32
)

// NewSecret returns a new random signing secret: SecretPrefix + base64(32 random bytes).
func NewSecret() (string, error) {
	b := make([]byte, secretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("signing: generate secret: %w", err)
	}
	return SecretPrefix + base64.StdEncoding.EncodeToString(b), nil
}
