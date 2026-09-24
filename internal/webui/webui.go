// Package webui serves Relay's embedded website, workspace, and operator console.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path/filepath"
)

//go:embed ui/*
var files embed.FS

// Handler returns the embedded same-origin web interface.
func Handler() http.Handler {
	assets, err := fs.Sub(files, "ui")
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /assets/{name}", func(w http.ResponseWriter, r *http.Request) {
		var asset string
		switch r.PathValue("name") {
		case "product.css":
			asset = "product.css"
		case "native-runs.png":
			asset = "native-runs.png"
		case "site.css":
			asset = "site.css"
		case "site.js":
			asset = "site.js"
		case "workspace.js":
			asset = "workspace.js"
		case "favicon.svg":
			asset = "favicon.svg"
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
		page := ""
		switch r.URL.Path {
		case "/":
			page = "index.html"
		case "/workspace":
			page = "workspace.html"
		case "/architecture":
			page = "architecture.html"
		case "/console":
			page = "console.html"
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFileFS(w, r, assets, page)
	})
	return mux
}

func mimeType(name string) string {
	if filepath.Ext(name) == ".png" {
		return "image/png"
	}
	if filepath.Ext(name) == ".svg" {
		return "image/svg+xml"
	}
	if filepath.Ext(name) == ".css" {
		return "text/css; charset=utf-8"
	}
	return "text/javascript; charset=utf-8"
}

// PublicHandler serves only product documentation; the operator console and
// mock testing workspace remain local to the full gateway.
func PublicHandler() http.Handler {
	full := Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/architecture", "/assets/product.css", "/assets/native-runs.png", "/assets/favicon.svg":
			full.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}
