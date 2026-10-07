package main

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// consoleDist embeds apps/console's Vite build output. The Dockerfile's Go
// build stage replaces this checked-in placeholder (see
// cmd/server/console/dist/index.html) with the real build before
// `go build` runs.
//
//go:embed console/dist
var consoleDist embed.FS

// consoleStaticHandler serves the business-owner console SPA under
// "/app/". It cannot be mounted at "/console/" — that prefix is already
// the console API (see main.go's mountCredentialedRoutes, "/console/" and
// "/auth/"). apps/console/vite.config.ts sets base: '/app/' so the
// build's own asset URLs (script src, link href, …) match this mount
// point; keep both in sync if this path ever changes.
//
// Any request under /app/ that doesn't match a real file falls back to
// /app/index.html (SPA client-side routing — see note in App.tsx: the
// console doesn't use a router library today, but this fallback is the
// same pattern apps/support and tripace's own web/admin use, so adding
// one later doesn't require touching this handler).
func consoleStaticHandler() http.Handler {
	sub, err := fs.Sub(consoleDist, "console/dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.StripPrefix("/app/", http.FileServer(http.FS(sub)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/app/"), "/")
		if name == "" {
			name = "."
		}
		if f, err := sub.Open(name); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/app/"
		fileServer.ServeHTTP(w, r2)
	})
}
