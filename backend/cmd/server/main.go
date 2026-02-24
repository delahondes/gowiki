package main

import (
	"flag"
	"log"
	"net/http"
	"path/filepath"

	"gowiki/backend/internal/api"
	"gowiki/backend/internal/auth"
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

	userStore, err := auth.NewUserStore(metaRoot)
	if err != nil {
		log.Fatalf("init user store: %v", err)
	}
	sessionStore := auth.NewSessionStore()

	router := api.NewRouter(store, mediaStore, store, searchIndex, store.Attic, store.Drafts, store, userStore, sessionStore, *serveWeb, filepath.Clean(*webDir))
	log.Printf("gowiki backend listening on %s", *addr)
	if *serveWeb {
		log.Printf("serving frontend assets from %s", *webDir)
	}
	log.Printf("content root: %s", contentRoot)
	log.Printf("meta root: %s", metaRoot)

	// Rebuild indexes and run attic migration in the background.
	go func() {
		log.Printf("rebuilding indexes...")
		if err := store.RebuildIndexes(); err != nil {
			log.Printf("WARNING: rebuild indexes failed: %v", err)
		} else {
			log.Printf("indexes rebuilt")
		}

		log.Printf("running attic migration (if needed)...")
		if err := store.Attic.MigrateExistingPages(contentRoot, metaRoot, store.Changelog); err != nil {
			log.Printf("WARNING: attic migration failed: %v", err)
		} else {
			log.Printf("attic migration complete")
		}
	}()

	if err := http.ListenAndServe(*addr, router); err != nil {
		log.Fatalf("http server failed: %v", err)
	}
}
