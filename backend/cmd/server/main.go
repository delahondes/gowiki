package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"gowiki/backend/internal/api"
	"gowiki/backend/internal/auth"
	"gowiki/backend/internal/config"
	"gowiki/backend/internal/database"
	"gowiki/backend/internal/storage"
	"gowiki/backend/internal/todo"
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

	// Initialize media version tracking.
	mediaVersionStore := storage.NewMediaVersionStore(metaRoot)
	if err := mediaVersionStore.Load(); err != nil {
		log.Fatalf("init media version store: %v", err)
	}
	mediaStore.VersionStore = mediaVersionStore
	store.MediaVersionStore = mediaVersionStore

	// Initialize media attic for archiving old media versions.
	mediaDataDir := filepath.Dir(contentRoot)
	mediaAttic := storage.NewMediaAttic(mediaDataDir)
	mediaStore.MediaAttic = mediaAttic

	userStore, err := auth.NewUserStore(metaRoot)
	if err != nil {
		log.Fatalf("init user store: %v", err)
	}
	groupStore, err := auth.NewGroupStore(metaRoot)
	if err != nil {
		log.Fatalf("init group store: %v", err)
	}
	sessionStore := auth.NewSessionStore()

	aclStore, err := auth.NewACLStore(metaRoot)
	if err != nil {
		log.Fatalf("init acl store: %v", err)
	}

	// Load site configuration (creates default config.yaml if absent).
	dataRoot := filepath.Dir(contentRoot)
	configPath := filepath.Join(dataRoot, "config.yaml")
	configStore, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("init config: %v", err)
	}
	log.Printf("config: %s", configPath)

	// Initialize database pool.
	dbPool := database.NewPool()
	cfg := configStore.Get()
	if cfg.Database.Enabled && cfg.Database.DSN != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := dbPool.Connect(ctx, cfg.Database.DSN); err != nil {
			log.Printf("WARNING: database connection failed: %v", err)
		} else {
			log.Printf("database: connected")
			if err := database.RunMigrations(ctx, dbPool); err != nil {
				log.Printf("WARNING: database migration failed: %v", err)
			} else {
				log.Printf("database: migrations applied")
				schemaStore := database.NewSchemaStore(dbPool)
				dataStore := database.NewDataStore(dbPool, schemaStore)
				store.DatabaseSync = database.NewDatabaseSync(schemaStore, dataStore)
			}
		}
		cancel()
	}

	// Initialize todo plugin: auto-enabled when database is connected,
	// unless explicitly disabled via todo.disabled config flag.
	var todoService *todo.TodoService
	todoEnabled := dbPool.IsConnected() && (cfg.Todo.Enabled || !cfg.Todo.Disabled)
	if todoEnabled {
		todoCtx, todoCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := todo.RunMigrations(todoCtx, dbPool); err != nil {
			log.Printf("WARNING: todo migration failed: %v", err)
		} else {
			log.Printf("todo plugin: migrations applied")
			todoStore := todo.NewTodoStore(dbPool)
			todoHub := todo.NewHub()
			dispatcher := todo.NewDispatcher(cfg.Todo.Notify, cfg.Site.Title)
			todoService = todo.NewService(todoStore, todoHub, dispatcher)
			store.TodoSync = todo.NewTodoSyncer(todoStore, todoHub, dispatcher)
			go todo.RunScheduler(context.Background(), todoStore, dispatcher, cfg.Todo.ReminderHours)
			log.Printf("todo plugin: active")
		}
		todoCancel()
	}

	// Initialize tag index.
	tagIndex := storage.NewTagIndex(metaRoot)
	if err := tagIndex.Load(); err != nil {
		log.Printf("WARNING: tag index load failed: %v", err)
	}
	store.TagIndex = tagIndex

	// Initialize headless Chrome for PDF export.
	browserCtx, browserCancel := api.InitBrowser()
	defer browserCancel()

	router := api.NewRouter(store, mediaStore, store, searchIndex, store.Attic, store.Drafts, store, mediaAttic, mediaVersionStore, configStore, userStore, groupStore, sessionStore, aclStore, store.Changelog, dbPool, tagIndex, browserCtx, browserCancel, *serveWeb, filepath.Clean(*webDir), todoService)
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
