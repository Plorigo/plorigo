package sshkeys

import (
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGenerate_ProducesUsableKeypair(t *testing.T) {
	kp, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// The private key parses back into a usable signer (what the SSH runner needs).
	signer, err := ssh.ParsePrivateKey(kp.PrivatePEM)
	if err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}
	if got := signer.PublicKey().Type(); got != ssh.KeyAlgoED25519 {
		t.Errorf("key type = %q, want %q", got, ssh.KeyAlgoED25519)
	}

	// The authorized_keys line is the public key for that same signer, with no trailing
	// newline (the caller adds framing when writing the file).
	if !strings.HasPrefix(kp.AuthorizedKey, "ssh-ed25519 ") {
		t.Errorf("authorized key = %q, want ssh-ed25519 prefix", kp.AuthorizedKey)
	}
	if strings.ContainsAny(kp.AuthorizedKey, "\n\r") {
		t.Errorf("authorized key must be a single trimmed line, got %q", kp.AuthorizedKey)
	}
	parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(kp.AuthorizedKey))
	if err != nil {
		t.Fatalf("ParseAuthorizedKey: %v", err)
	}
	if ssh.FingerprintSHA256(parsed) != kp.Fingerprint {
		t.Errorf("fingerprint %q does not match the public key", kp.Fingerprint)
	}
	if string(parsed.Marshal()) != string(signer.PublicKey().Marshal()) {
		t.Error("authorized key does not match the private key's public half")
	}

	if !strings.HasPrefix(kp.Fingerprint, "SHA256:") {
		t.Errorf("fingerprint = %q, want SHA256: prefix", kp.Fingerprint)
	}
}

func TestGenerate_IsRandomEachCall(t *testing.T) {
	a, err := Generate()
	if err != nil {
		t.Fatalf("Generate a: %v", err)
	}
	b, err := Generate()
	if err != nil {
		t.Fatalf("Generate b: %v", err)
	}
	if a.Fingerprint == b.Fingerprint {
		t.Error("two generated keys share a fingerprint; keygen is not random")
	}
}
