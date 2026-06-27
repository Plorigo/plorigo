// Package sshkeys generates the ed25519 keypair used for Plorigo's SSH server-management
// channel. It is the focused primitive behind the serversetup module: the private key is
// sealed at rest by the crypto box and never returned through any API, while the public
// key (an authorized_keys line) and its fingerprint are non-secret metadata. This key is
// deliberately distinct from the agent's job-signing key — the two never share material.
// See docs/architecture/server-management.md.
package sshkeys

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// KeyPair is a freshly generated SSH management keypair.
type KeyPair struct {
	// PrivatePEM is the OpenSSH PEM-encoded private key. It is sealed at rest by the
	// caller and MUST NOT be logged or returned through any API.
	PrivatePEM []byte
	// AuthorizedKey is the public "ssh-ed25519 AAAA…" line installed into the server's
	// authorized_keys. Non-secret. Trimmed of the trailing newline.
	AuthorizedKey string
	// Fingerprint is the SHA256 fingerprint of the public key (e.g. "SHA256:…"), surfaced
	// so a user can verify out of band which key is installed.
	Fingerprint string
}

// Generate creates a new ed25519 SSH keypair for the management channel.
func Generate() (KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, fmt.Errorf("sshkeys: generate ed25519: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return KeyPair{}, fmt.Errorf("sshkeys: marshal private key: %w", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return KeyPair{}, fmt.Errorf("sshkeys: new public key: %w", err)
	}
	return KeyPair{
		PrivatePEM:    pem.EncodeToMemory(block),
		AuthorizedKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))),
		Fingerprint:   ssh.FingerprintSHA256(sshPub),
	}, nil
}
