package server

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var embeddedFS embed.FS

// DistFileSystem returns an http.FileSystem serving the embedded React 19 web cockpit.
func DistFileSystem() http.FileSystem {
	sub, err := fs.Sub(embeddedFS, "dist")
	if err != nil {
		panic(err)
	}
	return http.FS(sub)
}
