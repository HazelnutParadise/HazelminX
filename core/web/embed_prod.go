//go:build embedweb

package web

import (
	"embed"
	"io/fs"
)

// CI copies web/dist into core/web/dist before building with -tags embedweb.
//
//go:embed all:dist
var dist embed.FS

func assets() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic("web: embedded dist missing: " + err.Error())
	}
	return sub
}
