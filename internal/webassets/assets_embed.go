//go:build embedassets

package webassets

import (
	"embed"
	"io/fs"
)

//go:embed dist
var embedded embed.FS

func Embedded() (fs.FS, bool) {
	dist, err := fs.Sub(embedded, "dist")
	return dist, err == nil
}
