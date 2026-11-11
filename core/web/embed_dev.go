//go:build !embedweb

package web

import "io/fs"

func assets() fs.FS { return nil }
