package main

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:web_build
var webFS embed.FS

func staticHandler() http.Handler {
	sub, err := fs.Sub(webFS, "web_build")
	if err != nil {
		panic("embedded web build not found: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		f, err := sub.Open(strings.TrimPrefix(path, "/"))
		if err != nil {
			// SPA fallback: serve index.html for client-side routing
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		f.Close()

		fileServer.ServeHTTP(w, r)
	})
}
