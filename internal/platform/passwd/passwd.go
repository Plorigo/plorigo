// Package passwd hashes and verifies passwords with argon2id, encoding the
// parameters and salt in the standard PHC string so the cost can evolve without a
// schema change. Memory-hard by design; see docs/architecture/auth.md.
package passwd

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// params are the argon2id cost parameters: memory-hard but still fast enough for
// interactive login on a modest self-host box.
type params struct {
	memory      uint32 // KiB
	iterations  uint32
	parallelism uint8
	saltLen     uint32
	keyLen      uint32
}

var defaults = params{memory: 64 * 1024, iterations: 3, parallelism: 2, saltLen: 16, keyLen: 32}

// ErrMismatch is returned by Verify when the password does not match the hash.
var ErrMismatch = errors.New("password does not match")

var b64 = base64.RawStdEncoding

// Hash returns a PHC-encoded argon2id hash of password.
func Hash(password string) (string, error) {
	salt := make([]byte, defaults.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("passwd: read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, defaults.iterations, defaults.memory, defaults.parallelism, defaults.keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, defaults.memory, defaults.iterations, defaults.parallelism,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify reports whether password matches the PHC-encoded argon2id hash. It
// returns nil on a match, ErrMismatch on a valid-but-wrong password, or another
// error if the encoded hash is malformed.
func Verify(password, encoded string) error {
	p, salt, key, err := decode(encoded)
	if err != nil {
		return err
	}
	other := argon2.IDKey([]byte(password), salt, p.iterations, p.memory, p.parallelism, uint32(len(key)))
	if subtle.ConstantTimeCompare(key, other) == 1 {
		return nil
	}
	return ErrMismatch
}

func decode(encoded string) (params, []byte, []byte, error) {
	// Layout: ["", "argon2id", "v=19", "m=65536,t=3,p=2", "<salt>", "<key>"].
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return params{}, nil, nil, errors.New("passwd: invalid hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return params{}, nil, nil, fmt.Errorf("passwd: invalid version: %w", err)
	}
	if version != argon2.Version {
		return params{}, nil, nil, fmt.Errorf("passwd: incompatible argon2 version %d", version)
	}
	var p params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.iterations, &p.parallelism); err != nil {
		return params{}, nil, nil, fmt.Errorf("passwd: invalid params: %w", err)
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return params{}, nil, nil, fmt.Errorf("passwd: invalid salt: %w", err)
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil {
		return params{}, nil, nil, fmt.Errorf("passwd: invalid key: %w", err)
	}
	return p, salt, key, nil
}
