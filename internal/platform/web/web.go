//go:build !embed_web

// Package web serves the dashboard. Without the `embed_web` build tag (the default
// `go build`), it serves a small built-in page so the binary is always runnable
// without a committed front-end artifact. Production builds use -tags embed_web.
package web

import "net/http"

// Handler serves the placeholder page in non-embedded builds.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(placeholder))
	})
}

const placeholder = `<!doctype html>
<html lang="en">
  <head><meta charset="utf-8"><title>Plorigo</title></head>
  <body style="font-family: system-ui; margin: 4rem auto; max-width: 40rem;">
    <h1>Plorigo control plane is running</h1>
    <p>This binary was built without the embedded dashboard.</p>
    <p>For local development run <code>pnpm --dir apps/web dev</code>.
       For a production single binary with the dashboard embedded, run
       <code>make build-embed</code>.</p>
  </body>
</html>`
