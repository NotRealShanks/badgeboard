package web

import (
	"embed"
	"io/fs"
)

//go:embed assets/*
var embeddedAssets embed.FS

// Assets returns the filesystem rooted at internal/web/assets
func Assets() fs.FS {
	sub, err := fs.Sub(embeddedAssets, "assets")
	if err != nil {
		panic("web: failed to locate embedded assets directory: " + err.Error())
	}
	return sub
}

