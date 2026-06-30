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
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitHub App support: a Plorigo deployment registered as a GitHub App can read PRIVATE repos and
// pull requests with short-lived, per-installation access tokens — never the user's broad OAuth
// token — and verify inbound webhook signatures. This is the credential path described in
// docs/architecture/security.md: the App private key and webhook secret live only in the control
// plane, are never returned by an RPC, never logged, and never sent to the agent.

// appJWTTTL is how long the App JWT is valid. GitHub caps it at 10 minutes; 9 leaves headroom.
const appJWTTTL = 9 * time.Minute

// instTokenRenewSkew refreshes a cached installation token this long before it actually expires, so
// a token handed to a caller is never about to lapse mid-request.
const instTokenRenewSkew = time.Minute

// instToken is a cached installation access token and the instant it expires.
type instToken struct {
	token   string
	expires time.Time
}

// appCreds resolves the active GitHub App credentials: the dynamic provider when set (re-checked on
// each call, so an App registered at runtime takes effect without a restart), otherwise the static
// config. ok is false unless both an app id and a private key are present.
func (c *Client) appCreds(ctx context.Context) (appID, privateKeyPEM string, ok bool) {
	if c.appCredsFn != nil {
		id, pem, present := c.appCredsFn(ctx)
		return id, pem, present && id != "" && pem != ""
	}
	return c.appID, c.appKeyPEM, c.appID != "" && c.appKeyPEM != ""
}

// AppConfigured reports whether the client can act as a GitHub App (mint installation tokens). The
// OAuth/public methods work regardless.
func (c *Client) AppConfigured(ctx context.Context) bool {
	_, _, ok := c.appCreds(ctx)
	return ok
}

// InstallationToken returns a short-lived access token for a GitHub App installation, minting a new
// one (and caching it) when none is cached or the cached one is near expiry. The token grants the
// App's repository permissions on that installation — the path by which Plorigo reads a PRIVATE
// repo/PR. Callers must never return it through an RPC, log it, or send it to the agent.
func (c *Client) InstallationToken(ctx context.Context, installationID string) (string, error) {
	if !c.AppConfigured(ctx) {
		return "", errors.New("github: app credentials are not configured")
	}
	if strings.TrimSpace(installationID) == "" {
		return "", errors.New("github: an installation id is required")
	}

	c.instMu.Lock()
	defer c.instMu.Unlock()
	now := time.Now()
	if t, ok := c.instTokens[installationID]; ok && now.Before(t.expires.Add(-instTokenRenewSkew)) {
		return t.token, nil
	}

	jwt, err := c.appJWT(ctx, now)
	if err != nil {
		return "", err
	}
	token, expires, err := c.mintInstallationToken(ctx, jwt, installationID)
	if err != nil {
		return "", err
	}
	if c.instTokens == nil {
		c.instTokens = make(map[string]instToken)
	}
	c.instTokens[installationID] = instToken{token: token, expires: expires}
	return token, nil
}

// mintInstallationToken exchanges an App JWT for an installation access token via the GitHub API.
func (c *Client) mintInstallationToken(ctx context.Context, appJWT, installationID string) (string, time.Time, error) {
	endpoint := c.apiBaseURL + "/app/installations/" + url.PathEscape(installationID) + "/access_tokens"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("github: build installation token request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", acceptJSON)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("github: mint installation token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return "", time.Time{}, classify(resp)
	}
	var body struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", time.Time{}, fmt.Errorf("github: decode installation token: %w", err)
	}
	if body.Token == "" {
		return "", time.Time{}, errors.New("github: installation token response had no token")
	}
	expires, err := time.Parse(time.RFC3339, body.ExpiresAt)
	if err != nil {
		// A missing/odd expiry shouldn't fail the mint; assume the documented 1-hour lifetime.
		expires = time.Now().Add(time.Hour)
	}
	return body.Token, expires, nil
}

// Installation is the subset of a GitHub App installation Plorigo stores: the installation id and
// the account (user or org) it is installed on.
type Installation struct {
	ID        int64
	Account   string
	AccountID int64
}

// GetInstallation reads a GitHub App installation by id, authenticating as the App (JWT). It is how
// the install callback resolves the account a new installation belongs to.
func (c *Client) GetInstallation(ctx context.Context, installationID string) (Installation, error) {
	if !c.AppConfigured(ctx) {
		return Installation{}, errors.New("github: app credentials are not configured")
	}
	jwt, err := c.appJWT(ctx, time.Now())
	if err != nil {
		return Installation{}, err
	}
	endpoint := c.apiBaseURL + "/app/installations/" + url.PathEscape(installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Installation{}, fmt.Errorf("github: build installation request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", acceptJSON)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return Installation{}, fmt.Errorf("github: get installation: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return Installation{}, classify(resp)
	}
	var body struct {
		ID      int64 `json:"id"`
		Account struct {
			Login string `json:"login"`
			ID    int64  `json:"id"`
		} `json:"account"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Installation{}, fmt.Errorf("github: decode installation: %w", err)
	}
	return Installation{ID: body.ID, Account: body.Account.Login, AccountID: body.Account.ID}, nil
}

// ManifestConversion is the result of exchanging a temporary manifest code for a freshly created
// GitHub App: the new App's id, slug, private key PEM, webhook secret, and OAuth client
// credentials. This is the one-time payload of GitHub's App-manifest flow — the private key and
// webhook secret are sensitive and must be sealed at rest, never logged or returned by an RPC.
type ManifestConversion struct {
	AppID         int64
	Slug          string
	Name          string
	PrivateKeyPEM string
	WebhookSecret string
	ClientID      string
	ClientSecret  string
	HTMLURL       string
}

// ConvertManifest exchanges the temporary code GitHub appends to the manifest redirect for the new
// App's credentials (POST /app-manifests/{code}/conversions). The call needs no authentication —
// the single-use code is the credential, and it expires within an hour. See
// https://docs.github.com/apps/sharing-github-apps/registering-a-github-app-from-a-manifest.
func (c *Client) ConvertManifest(ctx context.Context, code string) (ManifestConversion, error) {
	if strings.TrimSpace(code) == "" {
		return ManifestConversion{}, errors.New("github: a manifest code is required")
	}
	endpoint := c.apiBaseURL + "/app-manifests/" + url.PathEscape(code) + "/conversions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return ManifestConversion{}, fmt.Errorf("github: build manifest conversion request: %w", err)
	}
	req.Header.Set("Accept", acceptJSON)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return ManifestConversion{}, fmt.Errorf("github: convert manifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return ManifestConversion{}, classify(resp)
	}
	var body struct {
		ID            int64  `json:"id"`
		Slug          string `json:"slug"`
		Name          string `json:"name"`
		PEM           string `json:"pem"`
		WebhookSecret string `json:"webhook_secret"`
		ClientID      string `json:"client_id"`
		ClientSecret  string `json:"client_secret"`
		HTMLURL       string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return ManifestConversion{}, fmt.Errorf("github: decode manifest conversion: %w", err)
	}
	if body.ID == 0 || body.PEM == "" {
		return ManifestConversion{}, errors.New("github: manifest conversion response was missing the app id or private key")
	}
	return ManifestConversion{
		AppID:         body.ID,
		Slug:          body.Slug,
		Name:          body.Name,
		PrivateKeyPEM: body.PEM,
		WebhookSecret: body.WebhookSecret,
		ClientID:      body.ClientID,
		ClientSecret:  body.ClientSecret,
		HTMLURL:       body.HTMLURL,
	}, nil
}

// appJWT mints a short-lived RS256 JSON Web Token that identifies the App to GitHub (the
// credential used to request installation tokens), using the active credentials. iat is backdated
// 60s to tolerate clock skew between Plorigo and GitHub, per GitHub's guidance.
func (c *Client) appJWT(ctx context.Context, now time.Time) (string, error) {
	appID, pem, ok := c.appCreds(ctx)
	if !ok {
		return "", errors.New("github: app credentials are not configured")
	}
	key, err := c.appPrivateKey(pem)
	if err != nil {
		return "", err
	}
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	claims := map[string]any{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(appJWTTTL).Unix(),
		"iss": appID,
	}
	return signRS256(header, claims, key)
}

// appPrivateKey parses + caches the App's RSA private key, keyed by the PEM it came from. It
// re-parses only when the PEM changes (e.g. after a runtime re-registration), so the common path is
// a cache hit without paying the parse on every token mint.
func (c *Client) appPrivateKey(pem string) (*rsa.PrivateKey, error) {
	c.appKeyMu.Lock()
	defer c.appKeyMu.Unlock()
	if c.appKey != nil && c.appKeyPEMCached == pem {
		return c.appKey, nil
	}
	key, err := parseRSAPrivateKey(pem)
	if err != nil {
		return nil, err
	}
	c.appKey = key
	c.appKeyPEMCached = pem
	return key, nil
}

// VerifyWebhookSignature reports whether signatureHeader is a valid HMAC-SHA256 of body keyed by
// secret — the X-Hub-Signature-256 GitHub sends with every webhook delivery. A missing secret or a
// malformed/empty header is always a failure (never a pass), and the comparison is constant-time so
// it leaks nothing by timing. This is a pure function: the secret comes from the control plane's
// configuration, not from the client.
func VerifyWebhookSignature(secret string, body []byte, signatureHeader string) bool {
	if secret == "" {
		return false
	}
	const prefix = "sha256="
	if !strings.HasPrefix(signatureHeader, prefix) {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(signatureHeader, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}

// signRS256 renders and signs a compact JWS (header.payload.signature) with RSASSA-PKCS1-v1_5 over
// SHA-256 — the RS256 algorithm GitHub requires for App JWTs.
func signRS256(header map[string]string, claims map[string]any, key *rsa.PrivateKey) (string, error) {
	hb, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("github: marshal jwt header: %w", err)
	}
	cb, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("github: marshal jwt claims: %w", err)
	}
	signingInput := b64url(hb) + "." + b64url(cb)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("github: sign jwt: %w", err)
	}
	return signingInput + "." + b64url(sig), nil
}

// parseRSAPrivateKey decodes a PEM-encoded RSA private key in either PKCS#1 (GitHub's downloaded
// `.pem`) or PKCS#8 form.
func parseRSAPrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(pemStr)))
	if block == nil {
		return nil, errors.New("github: app private key is not valid PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("github: parse app private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("github: app private key is not an RSA key")
	}
	return key, nil
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
