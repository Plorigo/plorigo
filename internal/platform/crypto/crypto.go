// Package crypto seals and opens small values with AES-256-GCM, keyed by the process
// master key (APP_MASTER_KEY). It is the encryption primitive behind the secrets
// module: secret values are sealed before they touch the database and opened only by
// a future deployment job — never returned through the API. The key never leaves this
// package. It mirrors the focused, single-purpose shape of the passwd package. See
// docs/architecture/security.md.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// keyLen is the AES-256 key size: APP_MASTER_KEY must decode to exactly this many bytes.
const keyLen = 32

// ErrMalformed is returned by Open when the input is too short, was sealed with a
// different key, or has been tampered with. The cause is deliberately not
// distinguished, so a caller cannot probe ciphertexts.
var ErrMalformed = errors.New("crypto: malformed or tampered ciphertext")

// Box seals and opens bytes with one AES-256-GCM key. It is safe for concurrent use.
type Box struct {
	aead cipher.AEAD
}

// NewBox builds a Box from the base64-encoded master key. The key must decode to
// exactly 32 bytes (an AES-256 key); generate one with `openssl rand -base64 32`. It
// returns a descriptive error if the key is missing, not base64, or the wrong length,
// so the control plane fails fast at startup rather than at first secret write.
func NewBox(masterKey string) (*Box, error) {
	key, err := base64.StdEncoding.DecodeString(masterKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: APP_MASTER_KEY must be base64-encoded (openssl rand -base64 32): %w", err)
	}
	if len(key) != keyLen {
		return nil, fmt.Errorf("crypto: APP_MASTER_KEY must decode to %d bytes, got %d (generate with `openssl rand -base64 32`)", keyLen, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new gcm: %w", err)
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext and returns nonce||ciphertext. Each call draws a fresh
// random nonce, so sealing the same plaintext twice yields different outputs.
func (b *Box) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("crypto: read nonce: %w", err)
	}
	// Seal appends the ciphertext to its first argument; passing nonce makes the
	// result nonce||ciphertext (a fresh allocation, since nonce is at capacity).
	return b.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Open reverses Seal, returning the plaintext. It returns ErrMalformed if sealed is
// too short or fails authentication (wrong key or tampered bytes).
func (b *Box) Open(sealed []byte) ([]byte, error) {
	ns := b.aead.NonceSize()
	if len(sealed) < ns {
		return nil, ErrMalformed
	}
	nonce, ciphertext := sealed[:ns], sealed[ns:]
	plaintext, err := b.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrMalformed
	}
	return plaintext, nil
}
