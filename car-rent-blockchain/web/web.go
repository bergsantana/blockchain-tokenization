// Package web embeds the pt-BR user interface.
package web

import (
	_ "embed"
	"net/http"
)

//go:embed index.html
var indexHTML []byte

// Index serves the single-page UI.
func Index() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(indexHTML)
	})
}
