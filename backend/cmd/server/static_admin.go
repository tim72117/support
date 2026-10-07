package main

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// adminDist embeds apps/admin's Vite build output. Unlike consoleDist/
// supportDist/landingDist, backend/Dockerfile does NOT currently build
// apps/admin and copy its dist/ here — this is an internal operator tool,
// not something that needs to ship with every deploy of the consumer-facing
// stack, so wiring it into the production image was left out of this
// change's scope (see this project's task notes). Until that's done, this
// embeds the checked-in placeholder below; run `npm run build` in
// apps/admin and replace cmd/server/admin/dist's contents to embed a real
// build for a local/manual deploy.
//
//go:embed admin/dist
var adminDist embed.FS

// adminStaticHandler serves the platform-admin back office SPA under
// "/admin/". Same SPA-fallback shape as consoleStaticHandler (see its own
// doc comment) — the only difference is the mount prefix and which
// embedded dist it serves. Does not collide with "/admin/api/" (the admin
// API, see internal/admin and main.go's mountCredentialedRoutes):
// http.ServeMux dispatches the more specific "/admin/api/" prefix to the
// API mux first, so only requests that don't match that prefix reach this
// handler.
func adminStaticHandler() http.Handler {
	sub, err := fs.Sub(adminDist, "admin/dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.StripPrefix("/admin/", http.FileServer(http.FS(sub)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/admin/"), "/")
		if name == "" {
			name = "."
		}
		if f, err := sub.Open(name); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/admin/"
		fileServer.ServeHTTP(w, r2)
	})
}
