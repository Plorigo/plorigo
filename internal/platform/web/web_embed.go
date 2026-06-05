//go:build embed_web

package web

import (
	"embed"
	"io/fs"
	"net/http"
)

// dist is the built dashboard embedded into the production binary. `make build-embed`
// builds apps/web/dist and copies it here; the Dockerfile does the same COPY for images.
// go:embed cannot reference parent directories, so the copy into this package is required.
//
//go:embed all:dist
var dist embed.FS

// Handler serves the embedded dashboard SPA in production builds.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
