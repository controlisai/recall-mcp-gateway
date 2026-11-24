package dashboard

import (
	"embed"
	"io/fs"
)

//go:embed all:static
var staticFiles embed.FS

// StaticFS returns the embedded static files filesystem.
func StaticFS() (fs.FS, error) {
	return fs.Sub(staticFiles, "static")
}
