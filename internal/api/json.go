package api

import (
	"encoding/json"
	"net/http"
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
