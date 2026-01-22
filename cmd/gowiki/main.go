package main

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"

	_ "github.com/delahondes/gowiki/core"
	"github.com/delahondes/gowiki/docmodel"
	"github.com/delahondes/gowiki/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

var (
	storageInstance *storage.FileStorage
	templates       *template.Template
)

func main() {
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
	doc, err := docmodel.ParseMarkdown([]byte(content))
	if err != nil {
		return "", err
	}
	return docmodel.RenderHTML(doc)
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

	var docJSON string

	if storageInstance.PageExists(pagePath) {
		page, err = storageInstance.GetPage(pagePath)
		if err != nil {
			http.Error(w, "Failed to load page", http.StatusInternalServerError)
			return
		}

		// Parse Markdown → DocModel
		doc, err := docmodel.ParseMarkdown([]byte(page.Content))
		if err != nil {
			log.Printf("Error parsing markdown: %v", err)
		} else {
			// Serialize DocModel → JSON
			buf, err := json.Marshal(doc)
			if err != nil {
				log.Printf("Error serializing docmodel: %v", err)
			} else {
				docJSON = string(buf)
			}
		}
	}

	// Render edit template
	data := struct {
		Title        string
		Path         string
		Page         *storage.Page
		DocModelJSON template.JS
		TemplateName string
	}{
		Title:        "Edit: " + pagePath + " - GoWiki",
		Path:         pagePath,
		Page:         page,
		DocModelJSON: template.JS(docJSON),
		TemplateName: "edit",
	}

	templates.ExecuteTemplate(w, "base.html", data)
}

func handleSave(w http.ResponseWriter, r *http.Request, pagePath string) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	raw := r.FormValue("docmodel")
	if raw == "" {
		http.Error(w, "Missing docmodel", http.StatusBadRequest)
		return
	}

	var doc docmodel.Node
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		http.Error(w, "Invalid docmodel JSON", http.StatusBadRequest)
		return
	}

	md, err := docmodel.EmitMarkdown(doc)
	if err != nil {
		http.Error(w, "Failed to serialize markdown", http.StatusInternalServerError)
		return
	}

	err = storageInstance.SavePage(pagePath, md)
	if err != nil {
		http.Error(w, "Failed to save page", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/"+pagePath, http.StatusFound)
}
