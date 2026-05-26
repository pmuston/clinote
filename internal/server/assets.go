package server

import (
	"embed"
	"io/fs"
)

//go:embed templates/*.html static/*
var assetsFS embed.FS

func staticFS() fs.FS {
	sub, err := fs.Sub(assetsFS, "static")
	if err != nil {
		panic(err) // embedded path is known at compile time
	}
	return sub
}

func templatesFS() fs.FS {
	sub, err := fs.Sub(assetsFS, "templates")
	if err != nil {
		panic(err)
	}
	return sub
}
