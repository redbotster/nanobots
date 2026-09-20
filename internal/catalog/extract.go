package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// ExtractEmbedded lays this binary's own catalog out on disk at dir, in the
// exact layout `nanobots up` expects a repo checkout to have (bots/,
// examples/swarms/, roles/roles.yaml) — so dir can stand in as a synthetic
// repo root wherever the real bots/ isn't found.
func ExtractEmbedded(dir string) error {
	src, err := FS()
	if err != nil {
		return err
	}
	return extract(src, dir)
}

// marker names the file extract writes with the content hash of what it
// last extracted, so a second call with unchanged content is one file read
// and nothing else — most starts of a released binary, whose version
// rarely changes between them.
const marker = ".catalog-hash"

func extract(src fs.FS, dir string) error {
	want, err := hashFS(src)
	if err != nil {
		return fmt.Errorf("hash the embedded catalog: %w", err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, marker)); err == nil && string(got) == want {
		return nil
	}
	// Removed and rebuilt whole, not patched in place: a stale file this
	// binary's older catalog wrote and its newer one no longer includes
	// (a bot removed from the catalog, say) would otherwise survive
	// forever, and there is no cheaper way to know it should not.
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clear the old extracted catalog: %w", err)
	}
	if err := copyFS(src, dir); err != nil {
		return fmt.Errorf("extract the catalog: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, marker), []byte(want), 0o644); err != nil {
		return fmt.Errorf("record the extracted catalog's hash: %w", err)
	}
	return nil
}

// hashFS fingerprints every file's path and content, so a rename with the
// same bytes still counts as a change — the marker's whole job is telling
// "this is what's on disk right now" from "it might not be", and a hash
// that missed a rename would answer that wrong.
func hashFS(src fs.FS) (string, error) {
	var paths []string
	if err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, p)
		}
		return nil
	}); err != nil {
		return "", err
	}
	// fs.WalkDir already visits in lexical order, but sorting explicitly
	// means the hash does not depend on that being true forever.
	sort.Strings(paths)

	h := sha256.New()
	for _, p := range paths {
		b, err := fs.ReadFile(src, p)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\n", p)
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFS(src fs.FS, dir string) error {
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}
