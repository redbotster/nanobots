package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// nonNil turns a nil slice into an empty one before it's marshaled. Go's
// encoding/json renders a nil slice as `null`, not `[]` — harmless in Go,
// but a real trap for a frontend that calls .map() on what a TypeScript
// type promised would always be an array (see internal/api/bots.go: a bot
// like `notify` that declares no services at all has a nil Services slice
// and used to crash BotBrick.tsx outright).
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// writeJSONCached is writeJSON plus a conditional GET.
//
// The Runs page and the approval poller both fetch GET /api/runs every two
// seconds. On a machine with 199 runs that response is 91KB, and it is
// byte-identical between polls almost every time — 45KB/s of JSON marshalled
// on the server, sent over the wire, and parsed by the browser, to tell it
// nothing changed. An open tab left alone for an hour costs about 160MB.
//
// So: hash the body, hand it back as an ETag, and answer 304 with no body
// when the caller says it already has that one. Standard HTTP, nothing
// invented, and it works for any list endpoint.
//
// Cache-Control is no-store rather than no-cache on purpose. With no-cache
// the browser may satisfy a revalidation from its own cache and hand
// JavaScript a 200 with a body — saving the bytes but not the parse or the
// re-render. no-store keeps the browser out of it entirely: our explicit
// If-None-Match goes up, a real 304 comes back, and the client can skip
// updating state at all.
func writeJSONCached(w http.ResponseWriter, r *http.Request, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-store")
	if match := r.Header.Get("If-None-Match"); match != "" && etagMatches(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}

// etagMatches handles the comma-separated list form of If-None-Match, and
// the weak-comparison "W/" prefix a proxy may add. Not doing this would
// silently disable the whole thing the moment anything sat in front of the
// daemon.
func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		if strings.TrimPrefix(candidate, "W/") == strings.TrimPrefix(etag, "W/") {
			return true
		}
	}
	return false
}
