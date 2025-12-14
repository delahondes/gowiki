package main

import (
	"html/template"
	"log"
	"net/http"
	"os"

	"bytes"

	"github.com/delahondes/gowiki/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

var (
	storageInstance  *storage.FileStorage
	templates        *template.Template
	markdownRenderer goldmark.Markdown
)

func main() {
	// Initialize markdown renderer with extensions
	markdownRenderer = goldmark.New(
		goldmark.WithExtensions(extension.GFM),
	)

	// Initialize storage
	storageInstance = storage.NewFileStorage("./wiki")

	// Load templates
	loadTemplates()

	// Initialize router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Static files
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	// Wiki routes
	r.Get("/{pagePath:*}", handlePageRequest)
	r.Post("/{pagePath:*}", handlePageRequest)

	// Start server
	port := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		port = ":" + p
	}

	log.Printf("Starting wiki server on %s", port)
	log.Printf("Wiki data stored in: %s", storageInstance.RootPath)
	log.Fatal(http.ListenAndServe(port, r))
}

func loadTemplates() {
	templates = template.Must(template.ParseGlob("web/templates/*.html"))
}

func renderMarkdown(content string) (string, error) {
	var buf bytes.Buffer
	if err := markdownRenderer.Convert([]byte(content), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func handlePageRequest(w http.ResponseWriter, r *http.Request) {
	pagePath := chi.URLParam(r, "pagePath")
	if pagePath == "" {
		pagePath = "start"
	}

	// Get action from query parameter
	action := r.URL.Query().Get("action")

	switch action {
	case "edit":
		handleEdit(w, r, pagePath)
	case "save":
		if r.Method == http.MethodPost {
			handleSave(w, r, pagePath)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		// View action (default)
		handleView(w, r, pagePath)
	}
}

func handleView(w http.ResponseWriter, r *http.Request, pagePath string) {
	// Try to get the page
	page, err := storageInstance.GetPage(pagePath)
	if err != nil {
		// Page doesn't exist - redirect to edit
		http.Redirect(w, r, "/"+pagePath+"?action=edit", http.StatusFound)
		return
	}

	// Render markdown content
	renderedContent := ""
	if page.Content != "" {
		var err error
		renderedContent, err = renderMarkdown(page.Content)
		if err != nil {
			log.Printf("Error rendering markdown: %v", err)
			renderedContent = "<p>Error rendering page content.</p>"
		}
	}

	// Render view template
	data := struct {
		Title           string
		Path            string
		Page            *storage.Page
		RenderedContent template.HTML
		TemplateName    string
	}{
		Title:           page.Title + " - GoWiki",
		Path:            pagePath,
		Page:            page,
		RenderedContent: template.HTML(renderedContent),
		TemplateName:    "view",
	}

	templates.ExecuteTemplate(w, "base.html", data)
}

func handleEdit(w http.ResponseWriter, r *http.Request, pagePath string) {
	// Try to get existing page
	var page *storage.Page
	var err error
	if storageInstance.PageExists(pagePath) {
		page, err = storageInstance.GetPage(pagePath)
		if err != nil {
			http.Error(w, "Failed to load page", http.StatusInternalServerError)
			return
		}
	}

	// Render edit template
	data := struct {
		Title        string
		Path         string
		Page         *storage.Page
		TemplateName string
	}{
		Title:        "Edit: " + pagePath + " - GoWiki",
		Path:         pagePath,
		Page:         page,
		TemplateName: "edit",
	}

	templates.ExecuteTemplate(w, "base.html", data)
}

func handleSave(w http.ResponseWriter, r *http.Request, pagePath string) {
	// Parse form
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	content := r.FormValue("content")
	if content == "" {
		http.Error(w, "Content cannot be empty", http.StatusBadRequest)
		return
	}

	// Save page
	err = storageInstance.SavePage(pagePath, content)
	if err != nil {
		http.Error(w, "Failed to save page: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect to view
	http.Redirect(w, r, "/"+pagePath, http.StatusFound)
}
