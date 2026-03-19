package main

import (
	"context"
	"crypto/tls"
	"flag"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"gowiki/backend/internal/api"
	"gowiki/backend/internal/auth"
	"gowiki/backend/internal/comment"
	"gowiki/backend/internal/config"
	"gowiki/backend/internal/database"
	"gowiki/backend/internal/manual"
	"gowiki/backend/internal/reviewflow"
	"gowiki/backend/internal/storage"
	"gowiki/backend/internal/todo"
)

func main() {
	var (
		configFile = flag.String("config", "", "path to config file (YAML)")
		addr       = flag.String("addr", "", "HTTP listen address (overrides config)")
		dataDir    = flag.String("data-dir", "", "data root directory (overrides config)")
		serveWeb   = flag.Bool("serve-web", false, "serve built frontend assets from disk")
		webDir     = flag.String("web-dir", "", "directory containing built frontend assets (overrides config)")
		tlsDomain  = flag.String("tls-domain", "", "domain for automatic TLS via Let's Encrypt (overrides config)")
	)
	flag.Parse()

	// Load config: from -config flag, or from data-dir/config.yaml (legacy), or defaults.
	var configPath string
	var configStore *config.Store
	if *configFile != "" {
		configPath = filepath.Clean(*configFile)
		var err error
		configStore, err = config.Load(configPath)
		if err != nil {
			log.Fatalf("init config: %v", err)
		}
	}

	// Resolve data root: CLI flag > config > legacy default.
	dataRoot := *dataDir
	if dataRoot == "" && configStore != nil {
		dataRoot = configStore.Get().DataDir
	}
	if dataRoot == "" {
		// Legacy default: ./backend/data (content root was ./backend/data/content).
		dataRoot = "./backend/data"
	}
	dataRoot = filepath.Clean(dataRoot)

	contentRoot := filepath.Join(dataRoot, "content")
	metaRoot := filepath.Join(dataRoot, "meta")

	// If no -config was given, load config from data root (legacy behavior).
	if configStore == nil {
		configPath = filepath.Join(dataRoot, "config.yaml")
		var err error
		configStore, err = config.Load(configPath)
		if err != nil {
			log.Fatalf("init config: %v", err)
		}
	}
	log.Printf("config: %s", configPath)

	cfg := configStore.Get()

	// Resolve server settings: CLI flags override config.
	listenAddr := cfg.Server.Addr
	if *addr != "" {
		listenAddr = *addr
	}
	if listenAddr == "" {
		listenAddr = ":8080"
	}

	resolvedTLSDomain := cfg.Server.TLSDomain
	if *tlsDomain != "" {
		resolvedTLSDomain = *tlsDomain
	}

	resolvedWebDir := cfg.Server.WebDir
	if *webDir != "" {
		resolvedWebDir = *webDir
	}
	if resolvedWebDir == "" {
		resolvedWebDir = "./frontend/dist"
	}

	serveWebEnabled := *serveWeb || cfg.Server.WebDir != ""

	// Start pprof server only when not in TLS mode (avoid exposing debug on public servers).
	if resolvedTLSDomain == "" {
		go func() {
			log.Printf("pprof: listening on :6060")
			log.Println(http.ListenAndServe(":6060", nil))
		}()
	}

	store, err := storage.NewFileStore(contentRoot)
	if err != nil {
		log.Fatalf("init page storage: %v", err)
	}

	// Bootstrap embedded user manual (only writes missing files).
	if n := manual.Bootstrap(contentRoot); n > 0 {
		log.Printf("manual: bootstrapped %d pages to wiki/manual/", n)
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
	mediaAttic := storage.NewMediaAttic(dataRoot)
	mediaStore.MediaAttic = mediaAttic

	userStore, err := auth.NewUserStore(metaRoot)
	if err != nil {
		log.Fatalf("init user store: %v", err)
	}
	groupStore, err := auth.NewGroupStore(metaRoot)
	if err != nil {
		log.Fatalf("init group store: %v", err)
	}
	aclStore, err := auth.NewACLStore(metaRoot)
	if err != nil {
		log.Fatalf("init acl store: %v", err)
	}

	// Session store — uses TTL from config.
	sessionTTL := auth.DefaultSessionTTL
	if cfg.Auth.SessionTTL != "" {
		if parsed, err := time.ParseDuration(cfg.Auth.SessionTTL); err == nil {
			sessionTTL = parsed
		}
	}
	sessionStore, err := auth.NewSessionStore(metaRoot, sessionTTL)
	if err != nil {
		log.Fatalf("init session store: %v", err)
	}

	// Initialize database pool.
	dbPool := database.NewPool()
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
			dispatcher := todo.NewDispatcher(configStore, func(username string) string {
			u, err := userStore.Get(username)
			if err != nil {
				return ""
			}
			return u.Email
		})
			todoService = todo.NewService(todoStore, todoHub, dispatcher)
			store.TodoSync = todo.NewTodoSyncer(todoStore, todoHub, dispatcher)
			go todo.RunScheduler(context.Background(), todoStore, dispatcher)
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

	// Initialize reviewflow plugin (always available; config controls behavior).
	rfStore := reviewflow.NewStore(metaRoot)
	reviewflowService := reviewflow.NewService(rfStore, store.Attic, configStore)
	reviewflowService.SetPageReader(store)
	certStore := reviewflow.NewCertStore(metaRoot)
	signingVerifier := reviewflow.NewSigningVerifier(configStore, certStore)
	reviewflowService.SetSigningVerifier(signingVerifier)
	reviewflowService.SetCertStore(certStore)
	if cfg.Reviewflow.Signing.Enabled {
		log.Printf("reviewflow plugin: X.509 signing enabled")
	}
	if todoService != nil {
		reviewflowService.SetTodoIntegrator(reviewflow.NewTodoAdapter(todoService))
		log.Printf("reviewflow plugin: todo integration active")
	}
	store.ReviewflowSync = reviewflowService
	log.Printf("reviewflow plugin: active")

	// Initialize comment plugin.
	commentStore := comment.NewStore(metaRoot)
	commentService := comment.NewService(commentStore)
	store.CommentStore = commentStore
	log.Printf("comment plugin: active")

	// Initialize API token store for AI Content API.
	tokenStore, err := auth.NewTokenStore(metaRoot)
	if err != nil {
		log.Fatalf("init token store: %v", err)
	}
	if cfg.AIAPI.Enabled {
		log.Printf("ai api: enabled (token auth active)")
	}

	// Initialize headless Chrome for PDF export.
	browserCtx, browserCancel := api.InitBrowser()
	defer browserCancel()

	router := api.NewRouter(store, mediaStore, store, searchIndex, store.Attic, store.Drafts, store, mediaAttic, mediaVersionStore, configStore, userStore, groupStore, sessionStore, aclStore, store.Changelog, dbPool, tagIndex, store, browserCtx, browserCancel, serveWebEnabled, filepath.Clean(resolvedWebDir), todoService, reviewflowService, commentService, tokenStore)
	if serveWebEnabled {
		log.Printf("serving frontend assets from %s", resolvedWebDir)
	}
	log.Printf("data root: %s", dataRoot)
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

	// Graceful shutdown on SIGINT/SIGTERM.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	if resolvedTLSDomain != "" {
		// TLS mode: autocert on :443, HTTP redirect + ACME on :80.
		certDir := filepath.Join(dataRoot, "certs")
		if err := os.MkdirAll(certDir, 0o700); err != nil {
			log.Fatalf("create cert dir: %v", err)
		}

		m := &autocert.Manager{
			Cache:      autocert.DirCache(certDir),
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(resolvedTLSDomain),
		}

		// HTTP server: ACME challenges + redirect.
		httpServer := &http.Server{
			Addr:    ":80",
			Handler: m.HTTPHandler(nil), // nil = redirect non-ACME to HTTPS
		}
		go func() {
			log.Printf("http: listening on :80 (ACME + redirect)")
			if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
				log.Fatalf("http server failed: %v", err)
			}
		}()

		// HTTPS server.
		httpsServer := &http.Server{
			Addr:    ":443",
			Handler: router,
			TLSConfig: &tls.Config{
				GetCertificate: m.GetCertificate,
				MinVersion:     tls.VersionTLS12,
			},
		}

		go func() {
			log.Printf("https: listening on :443 (domain: %s)", resolvedTLSDomain)
			if err := httpsServer.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
				log.Fatalf("https server failed: %v", err)
			}
		}()

		<-shutdown
		log.Printf("shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpsServer.Shutdown(ctx)
		httpServer.Shutdown(ctx)
	} else {
		// Plain HTTP mode.
		log.Printf("http: listening on %s", listenAddr)
		server := &http.Server{
			Addr:    listenAddr,
			Handler: router,
		}

		go func() {
			if err := server.ListenAndServe(); err != http.ErrServerClosed {
				log.Fatalf("http server failed: %v", err)
			}
		}()

		<-shutdown
		log.Printf("shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}

	log.Printf("server stopped")
}
