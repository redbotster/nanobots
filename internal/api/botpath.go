package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Turning a bot id from a request into a path under BotsDir.
//
// This looks like it should not need saying, and http.ServeMux is the reason
// it does. A route like
//
//	POST /api/bots/{id}/services/{serviceId}/connection
//
// matches on the *escaped* path, so "..%2f..%2fetc" is one segment and binds
// happily to {id} — but r.PathValue returns the *decoded* value, "../../etc".
// The router's idea of the path and the handler's idea of it disagree, and a
// filepath.Join on the handler's side walks straight out of BotsDir. Verified
// against the real mux, not assumed.
//
// This repo has already been bitten by exactly this once: handleGetBlob used
// to pass its path segment into a filepath.Join, and "%2e%2e%2f..." read
// arbitrary files off disk, including the dotenv holding every API key on
// the machine (see blobs.go). That fix went in at the blob store and nowhere
// else, which left the same shape live on the endpoint that *writes*
// nanobot.yaml. One helper, used everywhere an id from outside becomes a
// path, is the version of that fix that does not have to be rediscovered.
//
// Bot ids that come from os.ReadDir (setProviderOnBot, handleListBots) are
// already single directory names and do not need this — a check there would
// imply the entries might be hostile, which would be a lie about where they
// come from.

// catalogBotDir resolves bots/<id>, refusing anything that would leave
// BotsDir or name something that is not a bot in the catalog.
//
// Returns the same error for "escaped the directory" and "no such bot" on
// purpose: the caller's next move is identical either way, and a distinct
// message would confirm to a prober which paths exist.
func (s *Server) catalogBotDir(botID string) (string, error) {
	if botID == "" || strings.ContainsAny(botID, `/\`) || strings.Contains(botID, "..") {
		return "", fmt.Errorf("no bot %q in the catalog", botID)
	}
	root, err := filepath.Abs(s.BotsDir)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, botID)
	// Belt and braces over the string check above: filepath.Join cleans as
	// it goes, so this catches anything the literal checks missed on a
	// platform whose separator rules differ from the ones assumed there.
	if !strings.HasPrefix(dir, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("no bot %q in the catalog", botID)
	}
	return dir, nil
}

// catalogBotManifest resolves bots/<id>/nanobot.yaml, and confirms it exists.
func (s *Server) catalogBotManifest(botID string) (string, error) {
	dir, err := s.catalogBotDir(botID)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "nanobot.yaml")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("no bot %q in the catalog", botID)
	}
	return path, nil
}
