package api

import (
	"net/http"
	"strings"
)

// safeBlobMime decides what Content-Type to serve a blob as.
//
// The mime comes from a query parameter, so it is caller-controlled, and
// this daemon answers on a fixed loopback port with
// Access-Control-Allow-Origin: * and no auth. Echoing an arbitrary
// Content-Type would let any page the user visits ask for a blob rendered as
// text/html and get scripted content executing on 127.0.0.1:7474's origin.
// Blobs are generated artifacts — PDFs, images, CSVs, plain text — so an
// allowlist costs nothing real and closes that off. Anything unrecognised
// downloads as bytes.
func safeBlobMime(requested string) string {
	// Strip any ";charset=..." or other parameters before matching.
	base := strings.ToLower(strings.TrimSpace(strings.SplitN(requested, ";", 2)[0]))
	switch base {
	case "application/pdf",
		"application/json",
		"text/plain",
		"text/csv",
		"text/markdown",
		"image/png",
		"image/jpeg",
		"image/gif",
		"image/webp",
		"image/svg+xml":
		// SVG is deliberately absent from the inline-safe set below: it can
		// carry script. It's allowed as a type but still served as an
		// attachment via Content-Disposition.
		return base
	}
	return "application/octet-stream"
}

// handleGetBlob serves a blob by its sha256 hash (the part of an nbf://
// reference after "nbf://sha256/") — how the WebUI offers a run's file
// outputs (e.g. the recap PDF) for download or inline viewing.
//
// The hash is validated by FSBlobStore.Read, which requires 64 hex
// characters. That check lives there rather than only here because it's a
// property of the URI format; before it existed, this handler passed the
// path segment straight into a filepath.Join and a request for
// "%2e%2e%2f..." read arbitrary files off disk — including the dotenv file
// holding every API key on the machine.
func (s *Server) handleGetBlob(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("uri")
	data, err := s.Blobs.Read("nbf://sha256/" + hash)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	mime := safeBlobMime(r.URL.Query().Get("mime"))
	w.Header().Set("Content-Type", mime)
	// Belt and braces on top of the allowlist: never let a browser sniff its
	// way to a different type than the one named here.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if mime == "image/svg+xml" || mime == "application/octet-stream" {
		w.Header().Set("Content-Disposition", "attachment")
	}
	w.Write(data)
}
