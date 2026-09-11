// Package webui serves Relay's embedded operator console.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path/filepath"
)

//go:embed ui/*
var files embed.FS

// Handler returns a dependency-free, same-origin web console.
func Handler() http.Handler {
	assets, err := fs.Sub(files, "ui")
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /assets/{name}", func(w http.ResponseWriter, r *http.Request) {
		var asset string
		switch r.PathValue("name") {
		case "styles.css":
			asset = "styles.css"
		case "app.js":
			asset = "app.js"
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", mimeType(asset))
		http.ServeFileFS(w, r, assets, asset)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFileFS(w, r, assets, "index.html")
	})
	return mux
}

func mimeType(name string) string {
	if filepath.Ext(name) == ".css" {
		return "text/css; charset=utf-8"
	}
	return "text/javascript; charset=utf-8"
}
