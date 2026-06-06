package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// tokenBytes is the entropy in every opaque token (256 bits).
const tokenBytes = 32

// newOpaqueToken returns a URL-safe random token and its sha256 hash. The raw
// token is handed to the client (cookie, email link); only the hash is stored, so
// a database leak never exposes a usable credential.
func newOpaqueToken() (raw string, hash []byte, err error) {
	b := make([]byte, tokenBytes)
	if _, err = rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("auth: read random: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw), nil
}

// newAPIToken returns a prefixed API token ("plk_..."), its display prefix, and
// its sha256 hash.
func newAPIToken() (raw, prefix string, hash []byte, err error) {
	b := make([]byte, tokenBytes)
	if _, err = rand.Read(b); err != nil {
		return "", "", nil, fmt.Errorf("auth: read random: %w", err)
	}
	raw = "plk_" + base64.RawURLEncoding.EncodeToString(b)
	prefix = raw[:12] // "plk_" + 8 chars, safe to display
	return raw, prefix, hashToken(raw), nil
}

// hashToken is the one-way function applied to every token before storage or
// lookup. A presented credential is hashed and compared to the stored hash.
func hashToken(raw string) []byte {
	h := sha256.Sum256([]byte(raw))
	return h[:]
}
