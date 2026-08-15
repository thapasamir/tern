// Package secretbox encrypts small secrets (such as webhook signing keys) before they are stored.
//
// Values are sealed with AES-256-GCM and stored as "enc:v1:<base64(nonce || ciphertext)>". The
// prefix names the scheme, so a future key or algorithm change can be told apart from old rows.
// A stored value without the prefix is treated as legacy plaintext and returned as is, so rows
// written before encryption was introduced keep working and are protected the next time they
// are rewritten (for example when the secret is rotated).
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const (
	// KeyBytes is the key length AES-256 needs.
	KeyBytes = 32

	sealedPrefix = "enc:v1:"
)

// ErrDecrypt is returned when a stored value cannot be opened: a wrong key, or a damaged value.
var ErrDecrypt = errors.New("secretbox: cannot decrypt stored value")

// Box seals and opens values with one key. It is safe for concurrent use.
type Box struct {
	aead cipher.AEAD
}

// New creates a Box from a base64-encoded 32-byte key (generate one with `openssl rand -base64 32`).
func New(base64Key string) (*Box, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(base64Key))
	if err != nil {
		return nil, fmt.Errorf("secretbox: key is not valid base64: %w", err)
	}
	if len(key) != KeyBytes {
		return nil, fmt.Errorf("secretbox: key must be %d bytes, got %d", KeyBytes, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretbox: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretbox: %w", err)
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext. Sealing the same value twice gives different results (random nonce).
func (b *Box) Seal(plaintext string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secretbox: generate nonce: %w", err)
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return sealedPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Open returns the plaintext of a stored value. A value that was never sealed (legacy
// plaintext) is returned unchanged.
func (b *Box) Open(stored string) (string, error) {
	encoded, sealed := strings.CutPrefix(stored, sealedPrefix)
	if !sealed {
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) < b.aead.NonceSize() {
		return "", ErrDecrypt
	}
	nonce, ciphertext := raw[:b.aead.NonceSize()], raw[b.aead.NonceSize():]
	plain, err := b.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plain), nil
}
