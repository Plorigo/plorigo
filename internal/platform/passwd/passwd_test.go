package passwd

import (
	"errors"
	"testing"
)

func TestHashVerifyRoundTrip(t *testing.T) {
	const pw = "correct horse battery staple"
	h, err := Hash(pw)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if h == pw {
		t.Fatal("hash must not equal the plaintext")
	}
	if err := Verify(pw, h); err != nil {
		t.Fatalf("Verify (correct password): %v", err)
	}
	if err := Verify("wrong password", h); !errors.Is(err, ErrMismatch) {
		t.Fatalf("Verify (wrong password): got %v, want ErrMismatch", err)
	}
}

func TestHashIsSalted(t *testing.T) {
	a, err := Hash("same")
	if err != nil {
		t.Fatalf("Hash a: %v", err)
	}
	b, err := Hash("same")
	if err != nil {
		t.Fatalf("Hash b: %v", err)
	}
	if a == b {
		t.Fatal("two hashes of the same password must differ (random salt)")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	for _, bad := range []string{"", "not-a-phc-string", "$argon2id$v=19$bad$x$y"} {
		if err := Verify("x", bad); err == nil {
			t.Errorf("Verify(%q): expected error, got nil", bad)
		}
	}
}
