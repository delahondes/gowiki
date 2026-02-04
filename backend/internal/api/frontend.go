package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) handleFrontend(w http.ResponseWriter, r *http.Request) {
	if !s.serveWeb {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}

	requestPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if requestPath == "." {
		requestPath = "index.html"
	}

	fullPath := filepath.Join(s.webDirPath, requestPath)
	if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
		http.ServeFile(w, r, fullPath)
		return
	}

	indexPath := filepath.Join(s.webDirPath, "index.html")
	http.ServeFile(w, r, indexPath)
}
