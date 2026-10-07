package main

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// supportDist embeds apps/support's Vite build output. The Dockerfile's Go
// build stage replaces this checked-in placeholder (see
// cmd/server/support/dist/index.html) with the real build before
// `go build` runs.
//
//go:embed support/dist
var supportDist embed.FS

// supportStaticHandler serves the consumer chat SPA under "/support/".
// apps/support/src/App.tsx reads the business slug straight out of
// window.location.pathname with the pattern /support/<slug> (no router
// library), so this prefix is exactly what the frontend already expects —
// nothing to reconcile here, unlike the console (see static_console.go).
// apps/support/vite.config.ts sets base: '/support/' so build asset URLs
// match this mount point.
//
// Any request under /support/ that isn't a real static asset (i.e. every
// /support/<slug> URL, plus any unknown path) falls back to
// /support/index.html so the SPA can read the slug and render.
func supportStaticHandler() http.Handler {
	sub, err := fs.Sub(supportDist, "support/dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.StripPrefix("/support/", http.FileServer(http.FS(sub)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/support/"), "/")
		if name == "" {
			name = "."
		}
		if f, err := sub.Open(name); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/support/"
		fileServer.ServeHTTP(w, r2)
	})
}
