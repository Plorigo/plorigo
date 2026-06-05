// Package principal carries the authenticated caller's identity through a
// request context. It is a neutral platform package (like problem and id) so
// auth, policy, and every privileged module share one identity seam without
// importing each other. The interceptor in internal/app populates it; handlers
// read it. See docs/architecture/auth.md.
package principal

import "context"

// Method is how the caller authenticated.
type Method string

const (
	// MethodSession is a browser session cookie.
	MethodSession Method = "session"
	// MethodAPIToken is a CLI/agent bearer token.
	MethodAPIToken Method = "api_token"
)

// Principal is the authenticated caller. The zero value is the anonymous caller.
type Principal struct {
	UserID string
	Method Method
}

// IsAuthenticated reports whether a caller identity is present.
func (p Principal) IsAuthenticated() bool { return p.UserID != "" }

type contextKey struct{}

// NewContext returns a copy of ctx carrying p.
func NewContext(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}

// FromContext returns the principal carried by ctx, or the anonymous zero value
// if none is present.
func FromContext(ctx context.Context) Principal {
	p, _ := ctx.Value(contextKey{}).(Principal)
	return p
}
