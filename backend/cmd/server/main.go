package main

import (
	"flag"
	"log"
	"net/http"
	"path/filepath"

	"gowiki/backend/internal/api"
	"gowiki/backend/internal/storage"
)

func main() {
	var (
		addr     = flag.String("addr", ":8080", "HTTP listen address")
		dataDir  = flag.String("data-dir", "./backend/data/pages", "filesystem root for page markdown files")
		serveWeb = flag.Bool("serve-web", false, "serve built frontend assets from disk")
		webDir   = flag.String("web-dir", "./frontend/dist", "directory that contains built frontend assets")
	)
	flag.Parse()

	store, err := storage.NewFileStore(filepath.Clean(*dataDir))
	if err != nil {
		log.Fatalf("init storage: %v", err)
	}

	router := api.NewRouter(store, *serveWeb, filepath.Clean(*webDir))
	log.Printf("gowiki backend listening on %s", *addr)
	if *serveWeb {
		log.Printf("serving frontend assets from %s", *webDir)
	}
	if err := http.ListenAndServe(*addr, router); err != nil {
		log.Fatalf("http server failed: %v", err)
	}
}
