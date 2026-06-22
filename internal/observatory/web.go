package observatory

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed webapp/dist/* webapp/dist/assets/*
var webAssets embed.FS

var webFileServer = http.FileServer(http.FS(mustSubFS(webAssets, "webapp/dist")))

func serveWebAsset(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" || r.URL.Path == "/index.html" || strings.HasPrefix(r.URL.Path, "/assets/") {
		webFileServer.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

func mustSubFS(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
