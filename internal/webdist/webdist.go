package webdist

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

//go:embed all:dist/*
var distFS embed.FS

// RegisterWebUI mounts the embedded React dashboard static files on the chi router.
// If a requested file doesn't exist, it falls back to index.html to support SPA client-side routing.
func RegisterWebUI(r chi.Router) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return
	}
	fileServer := http.FileServer(http.FS(sub))

	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		path := strings.TrimPrefix(req.URL.Path, "/")
		if strings.HasPrefix(path, "v1/") || strings.HasPrefix(path, "api/") || strings.HasPrefix(path, "health") {
			http.NotFound(w, req)
			return
		}
		if path == "" {
			req.URL.Path = "/"
			fileServer.ServeHTTP(w, req)
			return
		}

		if path == "site.webmanifest" {
			w.Header().Set("Content-Type", "application/manifest+json")
			fileServer.ServeHTTP(w, req)
			return
		}
		if path == "sw.js" {
			w.Header().Set("Content-Type", "application/javascript")
			w.Header().Set("Service-Worker-Allowed", "/")
			fileServer.ServeHTTP(w, req)
			return
		}

		// Check if file exists in embedded filesystem
		f, err := sub.Open(path)
		if err == nil {
			f.Close()
			fileServer.ServeHTTP(w, req)
			return
		}

		// Fallback to index.html for SPA routes (e.g. /providers, /settings, /logs)
		req.URL.Path = "/"
		fileServer.ServeHTTP(w, req)
	})

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		req.URL.Path = "/"
		fileServer.ServeHTTP(w, req)
	})

	r.Get("/assets/*", func(w http.ResponseWriter, req *http.Request) {
		fileServer.ServeHTTP(w, req)
	})

	r.Get("/favicon.ico", func(w http.ResponseWriter, req *http.Request) {
		fileServer.ServeHTTP(w, req)
	})
}
