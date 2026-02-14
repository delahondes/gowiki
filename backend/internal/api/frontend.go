package api

import (
	"net/http"
	"os"
	"path"
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

	if canServeAsAttachmentPath(requestPath) {
		if attachmentPath, err := s.mediaStore.ResolvePath(requestPath); err == nil {
			if info, statErr := os.Stat(attachmentPath); statErr == nil && !info.IsDir() {
				http.ServeFile(w, r, attachmentPath)
				return
			}
		}
	}

	indexPath := filepath.Join(s.webDirPath, "index.html")
	http.ServeFile(w, r, indexPath)
}

func canServeAsAttachmentPath(requestPath string) bool {
	if requestPath == "" || requestPath == "index.html" {
		return false
	}
	base := path.Base(strings.TrimSpace(requestPath))
	ext := path.Ext(base)
	if ext == "" {
		return false
	}
	return true
}

func (s *Server) handleFilePath(w http.ResponseWriter, r *http.Request) {
	requestPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if requestPath == "." || requestPath == "" {
		http.NotFound(w, r)
		return
	}

	if s.serveWeb {
		webPath := filepath.Join(s.webDirPath, requestPath)
		if info, err := os.Stat(webPath); err == nil && !info.IsDir() {
			http.ServeFile(w, r, webPath)
			return
		}
	}

	if !canServeAsAttachmentPath(requestPath) {
		http.NotFound(w, r)
		return
	}

	attachmentPath, err := s.mediaStore.ResolvePath(requestPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	info, statErr := os.Stat(attachmentPath)
	if statErr != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, attachmentPath)
}
