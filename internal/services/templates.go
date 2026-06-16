package services

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
)

// databaseTemplate is a built-in managed database the control plane can provision as a
// PRIVATE service: it fixes the image, the port, and the credentials/config to generate and
// inject so the container starts ready to use. This is a small in-code catalogue (Postgres
// now; Redis and others are later slices) — there is no template registry table, and the
// image/credentials are control-plane-chosen so a caller can't smuggle an arbitrary image
// through the database path.
type databaseTemplate struct {
	id          string
	displayName string
	image       string
	port        int32
	scheme      string // connection URI scheme, e.g. "postgresql"
	user        string // the role/user the container is created with
	db          string // the default database the container creates
}

// databaseTemplates is the provisioning catalogue, keyed by template id.
var databaseTemplates = map[string]databaseTemplate{
	"postgres": {
		id:          "postgres",
		displayName: "PostgreSQL",
		image:       "postgres:16-alpine",
		port:        5432,
		scheme:      "postgresql",
		user:        "plorigo",
		db:          "app",
	},
}

// lookupDatabaseTemplate finds a template by id. ok is false for an unknown id.
func lookupDatabaseTemplate(id string) (databaseTemplate, bool) {
	t, ok := databaseTemplates[strings.TrimSpace(id)]
	return t, ok
}

// env builds the container environment that starts the database with the generated
// password. These become the service's env vars (the agent injects them at run time).
func (t databaseTemplate) env(password string) map[string]string {
	switch t.id {
	case "postgres":
		return map[string]string{
			"POSTGRES_USER":     t.user,
			"POSTGRES_PASSWORD": password,
			"POSTGRES_DB":       t.db,
		}
	default:
		return map[string]string{}
	}
}

// connectionURI is the string a sibling service uses to reach the database over the
// per-environment network: scheme://user:password@host:port/db. host is the service slug
// (its network alias), so it resolves only inside the environment.
func (t databaseTemplate) connectionURI(host, password string) string {
	u := url.URL{
		Scheme: t.scheme,
		User:   url.UserPassword(t.user, password),
		Host:   fmt.Sprintf("%s:%d", host, t.port),
		Path:   "/" + t.db,
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
