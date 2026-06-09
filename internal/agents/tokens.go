package agents

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// tokenBytes is the entropy in every opaque token (256 bits), matching internal/auth.
const tokenBytes = 32

// newRegistrationToken returns a one-time agent registration token ("plrt_…") and its
// sha256 hash. The raw token is shown once in the dashboard; only the hash is stored.
func newRegistrationToken() (raw string, hash []byte, err error) {
	return newPrefixedToken("plrt_")
}

// newAgentCredential returns a durable agent credential ("plag_…") and its sha256 hash.
// The raw credential is returned to the agent once at registration; only the hash is
// stored, so a database leak never exposes a usable credential.
func newAgentCredential() (raw string, hash []byte, err error) {
	return newPrefixedToken("plag_")
}

func newPrefixedToken(prefix string) (raw string, hash []byte, err error) {
	b := make([]byte, tokenBytes)
	if _, err = rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("agents: read random: %w", err)
	}
	raw = prefix + base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw), nil
}

// hashToken is the one-way function applied to every token before storage or lookup.
func hashToken(raw string) []byte {
	h := sha256.Sum256([]byte(raw))
	return h[:]
}
