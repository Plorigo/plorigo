package github

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testAppKey generates an RSA key and returns it with its PKCS#1 PEM, for App-JWT tests.
func testAppKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return key, string(pemBytes)
}

func TestAppJWT_ClaimsAndSignature(t *testing.T) {
	key, keyPEM := testAppKey(t)
	c := NewClient(Config{AppID: "12345", AppPrivateKeyPEM: keyPEM})
	now := time.Unix(1_700_000_000, 0)

	tok, err := c.appJWT(now)
	if err != nil {
		t.Fatalf("appJWT: %v", err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt has %d parts, want 3", len(parts))
	}

	// Header: RS256.
	var hdr map[string]string
	mustB64JSON(t, parts[0], &hdr)
	if hdr["alg"] != "RS256" || hdr["typ"] != "JWT" {
		t.Errorf("header = %v, want RS256/JWT", hdr)
	}

	// Claims: iss = app id, iat backdated 60s, exp ~9 min out. (numbers decode as float64).
	var claims map[string]any
	mustB64JSON(t, parts[1], &claims)
	if int64(claims["iat"].(float64)) != now.Add(-60*time.Second).Unix() {
		t.Errorf("iat = %v, want backdated 60s", claims["iat"])
	}
	if int64(claims["exp"].(float64)) != now.Add(appJWTTTL).Unix() {
		t.Errorf("exp = %v, want now + %s", claims["exp"], appJWTTTL)
	}
	if claims["iss"] != "12345" {
		t.Errorf("iss = %v, want the app id", claims["iss"])
	}

	// Signature verifies against the public key over header.payload.
	signingInput := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	digest := sha256.Sum256([]byte(signingInput))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Errorf("signature does not verify: %v", err)
	}
}

func TestAppJWT_RejectsBadKey(t *testing.T) {
	c := NewClient(Config{AppID: "1", AppPrivateKeyPEM: "not a pem"})
	if _, err := c.appJWT(time.Now()); err == nil {
		t.Fatal("appJWT with a bad key should error")
	}
}

func TestInstallationToken_MintsAndCaches(t *testing.T) {
	_, keyPEM := testAppKey(t)
	var calls atomic.Int32
	expiry := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/app/installations/42/access_tokens" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("missing App JWT bearer auth: %q", auth)
		}
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "ghs_installtoken", "expires_at": expiry})
	}))
	defer srv.Close()

	c := NewClient(Config{AppID: "1", AppPrivateKeyPEM: keyPEM, APIBaseURL: srv.URL})

	for i := 0; i < 3; i++ {
		tok, err := c.InstallationToken(context.Background(), "42")
		if err != nil {
			t.Fatalf("InstallationToken: %v", err)
		}
		if tok != "ghs_installtoken" {
			t.Fatalf("token = %q, want the minted token", tok)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("minted %d times, want 1 (the token should be cached)", calls.Load())
	}
}

func TestInstallationToken_RefreshesWhenNearExpiry(t *testing.T) {
	_, keyPEM := testAppKey(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		// Already past the renew skew, so the next call must re-mint.
		exp := time.Now().Add(30 * time.Second).UTC().Format(time.RFC3339)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": fmt.Sprintf("tok-%d", calls.Load()), "expires_at": exp})
	}))
	defer srv.Close()

	c := NewClient(Config{AppID: "1", AppPrivateKeyPEM: keyPEM, APIBaseURL: srv.URL})
	if _, err := c.InstallationToken(context.Background(), "42"); err != nil {
		t.Fatalf("first mint: %v", err)
	}
	if _, err := c.InstallationToken(context.Background(), "42"); err != nil {
		t.Fatalf("second mint: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("minted %d times, want 2 (a near-expiry token must be refreshed)", calls.Load())
	}
}

func TestInstallationToken_RequiresAppConfig(t *testing.T) {
	c := NewClient(Config{}) // no App credentials
	if _, err := c.InstallationToken(context.Background(), "42"); err == nil {
		t.Fatal("InstallationToken without App config should error")
	}
	if c.AppConfigured() {
		t.Error("AppConfigured should be false without credentials")
	}
}

func TestVerifyWebhookSignature(t *testing.T) {
	secret := "s3cr3t"
	body := []byte(`{"action":"opened"}`)
	// A correct signature, computed the same way GitHub does.
	good := signBody(secret, body)

	cases := []struct {
		name   string
		secret string
		body   []byte
		header string
		want   bool
	}{
		{"valid", secret, body, good, true},
		{"tampered body", secret, []byte(`{"action":"closed"}`), good, false},
		{"wrong secret", "other", body, good, false},
		{"no prefix", secret, body, strings.TrimPrefix(good, "sha256="), false},
		{"empty header", secret, body, "", false},
		{"empty secret", "", body, good, false},
		{"not hex", secret, body, "sha256=zzzz", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifyWebhookSignature(tc.secret, tc.body, tc.header); got != tc.want {
				t.Errorf("VerifyWebhookSignature = %v, want %v", got, tc.want)
			}
		})
	}
}

// signBody computes the X-Hub-Signature-256 header value GitHub sends, for the verify tests.
func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func mustB64JSON(t *testing.T, seg string, out any) {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		t.Fatalf("decode segment: %v", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("unmarshal segment: %v", err)
	}
}
