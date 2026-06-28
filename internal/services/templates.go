package services

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/plorigo/plorigo/internal/platform/problem"
)

// databaseTemplate is a built-in managed database the control plane can provision as a
// PRIVATE service: it fixes the image, the port, and the connection scheme, and supplies the
// DEFAULT credentials/config to generate and inject so the container starts ready to use. The
// database name and user are defaults the caller may override; the image and port are
// control-plane-chosen and never caller-supplied (so a caller can't smuggle an arbitrary image
// through the database path). This is a small in-code catalogue (Postgres now; Redis, MongoDB,
// and others are later slices — each adds an entry here plus a case in env()) — there is no
// template registry table.
type databaseTemplate struct {
	id          string
	displayName string
	image       string
	port        int32
	scheme      string // connection URI scheme, e.g. "postgresql"
	defaultUser string // the role/user the container is created with unless overridden
	defaultDB   string // the database the container creates unless overridden
}

// databaseTemplates is the provisioning catalogue, keyed by template id.
var databaseTemplates = map[string]databaseTemplate{
	"postgres": {
		id:          "postgres",
		displayName: "PostgreSQL",
		image:       "postgres:16-alpine",
		port:        5432,
		scheme:      "postgresql",
		defaultUser: "plorigo",
		defaultDB:   "app",
	},
}

// lookupDatabaseTemplate finds a template by id. ok is false for an unknown id.
func lookupDatabaseTemplate(id string) (databaseTemplate, bool) {
	t, ok := databaseTemplates[strings.TrimSpace(id)]
	return t, ok
}

// env builds the container environment that starts the database with the resolved user,
// password, and database name. These become the service's config variables (the agent injects
// them at run time). The switch maps the neutral (user, password, db) triple onto each image's
// expected variable names, so a new managed database is a new case here.
func (t databaseTemplate) env(user, password, db string) map[string]string {
	switch t.id {
	case "postgres":
		return map[string]string{
			"POSTGRES_USER":     user,
			"POSTGRES_PASSWORD": password,
			"POSTGRES_DB":       db,
		}
	default:
		return map[string]string{}
	}
}

// connectionURI is the string a sibling service uses to reach the database over the
// per-environment network: scheme://user:password@host:port/db. host is the service slug
// (its network alias), so it resolves only inside the environment.
func (t databaseTemplate) connectionURI(host, user, password, db string) string {
	u := url.URL{
		Scheme: t.scheme,
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%d", host, t.port),
		Path:   "/" + db,
	}
	return u.String()
}

// generateDatabasePassword returns a strong, URL-safe password (the RawURL base64 alphabet
// is unreserved, so it needs no escaping in a connection URI or a shell env value).
func generateDatabasePassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// pgIdentifierRe is the grammar for a Postgres role / database name we accept: an ASCII
// letter or underscore, then letters, digits, or underscores. It keeps user-chosen names
// safe to inject as env values and to place in a connection URI without quoting.
var pgIdentifierRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

// validatePGIdentifier rejects a database name / username that isn't a plain identifier
// (≤ 63 chars, the Postgres limit). label names the field for the error message.
func validatePGIdentifier(label, s string) error {
	if !pgIdentifierRe.MatchString(s) {
		return problem.InvalidInput("%s must be 1–63 chars: a letter or underscore, then letters, digits, or underscores", label)
	}
	return nil
}

// dbPasswordRe bounds a caller-supplied password: 8–128 printable ASCII characters with no
// spaces or control characters, so it is safe both as a connection-URI userinfo component and
// as a shell env value. A blank password is handled by the caller (it generates one instead).
var dbPasswordRe = regexp.MustCompile(`^[\x21-\x7E]{8,128}$`)

// validateDBPassword rejects a caller-supplied password outside the allowed grammar.
func validateDBPassword(s string) error {
	if !dbPasswordRe.MatchString(s) {
		return problem.InvalidInput("password must be 8–128 characters with no spaces or control characters")
	}
	return nil
}
