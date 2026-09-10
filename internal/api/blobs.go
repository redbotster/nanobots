package api

import (
	"net/http"
)

// handleGetBlob serves a blob by its sha256 hash (the part of an nbf://
// reference after "nbf://sha256/") — how the WebUI offers a run's file
// outputs (e.g. the recap PDF) for download or inline viewing.
func (s *Server) handleGetBlob(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("uri")
	data, err := s.Blobs.Read("nbf://sha256/" + hash)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	mime := r.URL.Query().Get("mime")
	if mime == "" {
		mime = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mime)
	w.Write(data)
}
