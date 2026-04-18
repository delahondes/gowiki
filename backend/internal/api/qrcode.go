package api

import (
	"net/http"
	"strconv"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// handleQRCode renders a QR code PNG for the given data.
//
//	GET /api/qrcode?data=<url-or-text>&size=<px>
//
// size is optional (default 256, clamped 64..1024). The response is a
// cacheable PNG. This is used by the PDF export footer to embed a scannable
// link to the source page.
func (s *Server) handleQRCode(w http.ResponseWriter, r *http.Request) {
	data := strings.TrimSpace(r.URL.Query().Get("data"))
	if data == "" {
		http.Error(w, "missing data parameter", http.StatusBadRequest)
		return
	}
	if len(data) > 2048 {
		http.Error(w, "data too long", http.StatusBadRequest)
		return
	}

	size := 256
	if raw := r.URL.Query().Get("size"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			size = v
		}
	}
	if size < 64 {
		size = 64
	}
	if size > 1024 {
		size = 1024
	}

	png, err := qrcode.Encode(data, qrcode.Medium, size)
	if err != nil {
		http.Error(w, "qrcode encode failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(png)
}
