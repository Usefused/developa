// Package webui serves the explorer without a separate frontend runtime.
package webui

import (
	"crypto/rand"
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist/*
var assets embed.FS

func Handler() http.Handler {
	files, _ := fs.Sub(assets, "dist/assets")
	static := http.StripPrefix("/assets/", http.FileServer(http.FS(files)))
	page, _ := assets.ReadFile("dist/index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			secureHeaders(w, "")
			static.ServeHTTP(w, r)
			return
		}
		if !pageRoute(r.URL.Path) {
			secureHeaders(w, "")
			http.NotFound(w, r)
			return
		}
		nonce := rand.Text()
		secureHeaders(w, nonce)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(strings.ReplaceAll(string(page), "__DEVELOPA_CSP_NONCE__", nonce)))
	})
}

func pageRoute(path string) bool {
	switch path {
	case "/", "/blocks", "/flow", "/changes", "/analysis", "/features", "/chain":
		return true
	default:
		return false
	}
}

func secureHeaders(w http.ResponseWriter, nonce string) {
	// Only framework bootstrap scripts receive a per-response nonce. Repository
	// text remains untrusted; neither inline handlers nor unsafe-inline are allowed.
	scripts := "'self'"
	if nonce != "" {
		scripts += " 'nonce-" + nonce + "'"
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src "+scripts+"; style-src 'self'; connect-src 'self'; img-src 'self'; font-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
}
