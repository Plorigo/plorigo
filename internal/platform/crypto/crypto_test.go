package crypto

import (
	"bytes"
	"encoding/base64"
	"testing"
)

// testKey decodes to the 32 bytes "plorigo-dev-only-not-a-secret-32".
const testKey = "cGxvcmlnby1kZXYtb25seS1ub3QtYS1zZWNyZXQtMzI="

// keyOfLen returns a base64 key that decodes to n bytes.
func keyOfLen(n int) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xAB}, n))
}

func newTestBox(t *testing.T, key string) *Box {
	t.Helper()
	b, err := NewBox(key)
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	return b
}

func TestNewBox_RejectsInvalidKeys(t *testing.T) {
	cases := map[string]string{
		"empty":      "",
		"not base64": "this is not base64 !!!",
		"16 bytes":   keyOfLen(16),
		"31 bytes":   keyOfLen(31),
		"33 bytes":   keyOfLen(33),
	}
	for name, key := range cases {
		if _, err := NewBox(key); err == nil {
			t.Errorf("%s: expected an error, got nil", name)
		}
	}
}

func TestNewBox_AcceptsValidKeys(t *testing.T) {
	for _, key := range []string{testKey, keyOfLen(32)} {
		if _, err := NewBox(key); err != nil {
			t.Errorf("NewBox(%q): unexpected error %v", key, err)
		}
	}
}

func TestSealOpen_RoundTrip(t *testing.T) {
	b := newTestBox(t, testKey)
	for _, pt := range [][]byte{
		[]byte("postgres://user:pw@host/db"),
		{}, // empty plaintext is valid
		[]byte("multi\nline\x00binary"),
	} {
		sealed, err := b.Seal(pt)
		if err != nil {
			t.Fatalf("Seal: %v", err)
		}
		got, err := b.Open(sealed)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if !bytes.Equal(got, pt) {
			t.Errorf("round-trip = %q, want %q", got, pt)
		}
	}
}

func TestSeal_IsNotPlaintextAndRandomized(t *testing.T) {
	b := newTestBox(t, testKey)
	pt := []byte("STRIPE_SECRET_KEY-value")
	c1, _ := b.Seal(pt)
	c2, _ := b.Seal(pt)
	if bytes.Contains(c1, pt) {
		t.Error("ciphertext must not contain the plaintext")
	}
	if bytes.Equal(c1, c2) {
		t.Error("sealing the same plaintext twice must differ (random nonce)")
	}
}

func TestOpen_RejectsTamperedAndShort(t *testing.T) {
	b := newTestBox(t, testKey)
	sealed, _ := b.Seal([]byte("secret"))

	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-1] ^= 0xFF
	if _, err := b.Open(tampered); err != ErrMalformed {
		t.Errorf("tampered: got %v, want ErrMalformed", err)
	}

	if _, err := b.Open(sealed[:b.aead.NonceSize()-1]); err != ErrMalformed {
		t.Errorf("short: got %v, want ErrMalformed", err)
	}
}

func TestOpen_RejectsWrongKey(t *testing.T) {
	sealed, _ := newTestBox(t, testKey).Seal([]byte("secret"))
	other := newTestBox(t, keyOfLen(32))
	if _, err := other.Open(sealed); err != ErrMalformed {
		t.Errorf("wrong key: got %v, want ErrMalformed", err)
	}
}
