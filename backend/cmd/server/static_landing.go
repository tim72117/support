package main

import (
	"embed"
	"io/fs"
	"net/http"
)

// landingDist embeds apps/landing's Vite build output. The Dockerfile's Go
// build stage replaces this checked-in placeholder (see
// cmd/server/landing/dist/index.html) with the real build before
// `go build` runs — the directory name must match exactly, since embed
// paths are resolved at compile time.
//
//go:embed landing/dist
var landingDist embed.FS

// landingStaticHandler serves apps/landing at the site root ("/"). Unlike
// the console/support SPAs, landing is a plain multi-page static site (see
// apps/landing/vite.config.ts: "Vite here is just a dev server + bundler
// for plain HTML/CSS/JS, not a framework app") with no client-side router,
// so there is no SPA fallback to index.html for unknown paths — a missing
// file is a real 404.
//
// Registered last (lowest priority) on the top-level mux: http.ServeMux
// matches the most specific pattern first, so this only ever receives
// requests that didn't match /console/, /auth/, /public/, /app/ or
// /support/.
func landingStaticHandler() http.Handler {
	sub, err := fs.Sub(landingDist, "landing/dist")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
