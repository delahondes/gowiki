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
		dataDir  = flag.String("data-dir", "./backend/data/content", "filesystem root for wiki content files (pages + attachments)")
		serveWeb = flag.Bool("serve-web", false, "serve built frontend assets from disk")
		webDir   = flag.String("web-dir", "./frontend/dist", "directory that contains built frontend assets")
	)
	flag.Parse()

	contentRoot := filepath.Clean(*dataDir)
	metaRoot := filepath.Join(filepath.Dir(contentRoot), "meta")

	store, err := storage.NewFileStore(contentRoot)
	if err != nil {
		log.Fatalf("init page storage: %v", err)
	}

	// Open search index.
	searchIndexPath := filepath.Join(metaRoot, "_search")
	searchIndex, err := storage.OpenSearchIndex(searchIndexPath)
	if err != nil {
		log.Fatalf("init search index: %v", err)
	}
	defer searchIndex.Close()
	store.SearchIndex = searchIndex

	mediaStore, err := storage.NewMediaFileStore(contentRoot)
	if err != nil {
		log.Fatalf("init media storage: %v", err)
	}

	router := api.NewRouter(store, mediaStore, store, searchIndex, store, *serveWeb, filepath.Clean(*webDir))
	log.Printf("gowiki backend listening on %s", *addr)
	if *serveWeb {
		log.Printf("serving frontend assets from %s", *webDir)
	}
	log.Printf("content root: %s", contentRoot)
	log.Printf("meta root: %s", metaRoot)

	// Rebuild indexes in the background so the server can accept requests immediately.
	// Page reads work without indexes; search results may be empty until rebuild completes.
	go func() {
		log.Printf("rebuilding indexes...")
		if err := store.RebuildIndexes(); err != nil {
			log.Printf("WARNING: rebuild indexes failed: %v", err)
		} else {
			log.Printf("indexes rebuilt")
		}
	}()

	if err := http.ListenAndServe(*addr, router); err != nil {
		log.Fatalf("http server failed: %v", err)
	}
}
