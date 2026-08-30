package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var files embed.FS

func Static() (fs.FS, error) {
	return fs.Sub(files, "static")
}

func ReadFile(name string) ([]byte, error) {
	return files.ReadFile("static/" + name)
}
