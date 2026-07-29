// Package frontend serves the compiled React application from the Go binary.
package frontend

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Run `pnpm build` in this directory before `go build`.
//
//go:embed all:dist
var bundle embed.FS

func StaticHandler() http.Handler {
	root, err := fs.Sub(bundle, "dist")
	if err != nil {
		panic("frontend: dist/ is missing, run pnpm build first")
	}

	files := http.FileServer(http.FS(root))

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		name := strings.TrimPrefix(path.Clean(request.URL.Path), "/")

		if strings.HasPrefix(name, "assets/") {
			writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}

		if name != "" && !exists(root, name) {
			index := request.Clone(request.Context())
			index.URL.Path = "/"
			files.ServeHTTP(writer, index)
			return
		}

		files.ServeHTTP(writer, request)
	})
}

func exists(root fs.FS, name string) bool {
	file, err := root.Open(name)
	if err != nil {
		return false
	}
	file.Close()
	return true
}
