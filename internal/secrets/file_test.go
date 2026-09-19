package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreRoundTripsAValueThroughEncryption(t *testing.T) {
	f := &File{Dir: t.TempDir()}

	if err := f.Put("slack/bot_token", "xoxb-real-value"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, found, err := f.Get("slack/bot_token")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("Get: found=false for a key that was just Put")
	}
	if got != "xoxb-real-value" {
		t.Fatalf("Get: got %q, want %q", got, "xoxb-real-value")
	}
}

func TestFileStoreOnDiskIsNotThePlaintext(t *testing.T) {
	dir := t.TempDir()
	f := &File{Dir: dir}

	if err := f.Put("github/token", "ghp_supersecret"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, storeFileName))
	if err != nil {
		t.Fatalf("read store file: %v", err)
	}
	if contains(raw, "ghp_supersecret") {
		t.Fatal("the store file on disk contains the plaintext secret — encryption did nothing")
	}
}

func TestFileStoreMissingKeyIsNotFoundNotError(t *testing.T) {
	f := &File{Dir: t.TempDir()}

	_, found, err := f.Get("nothing/here")
	if err != nil {
		t.Fatalf("Get on an empty store returned an error, want found=false: %v", err)
	}
	if found {
		t.Fatal("Get: found=true for a key never written")
	}
}

func TestFileStoreDeleteRemovesTheKeyButKeepsOthers(t *testing.T) {
	f := &File{Dir: t.TempDir()}
	if err := f.Put("a", "1"); err != nil {
		t.Fatalf("Put a: %v", err)
	}
	if err := f.Put("b", "2"); err != nil {
		t.Fatalf("Put b: %v", err)
	}

	if err := f.Delete("a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, found, _ := f.Get("a"); found {
		t.Fatal("a still found after Delete")
	}
	if v, found, _ := f.Get("b"); !found || v != "2" {
		t.Fatalf("Delete of a disturbed b: found=%v v=%q", found, v)
	}
}

func TestFileStoreSameIdentityPersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()

	first := &File{Dir: dir}
	if err := first.Put("k", "v"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// A fresh *File pointed at the same directory is what a second CLI
	// invocation or a daemon restart actually does — it must read the
	// identity this process already generated, not mint a new one that
	// can't decrypt the existing store.
	second := &File{Dir: dir}
	got, found, err := second.Get("k")
	if err != nil {
		t.Fatalf("Get from a fresh instance: %v", err)
	}
	if !found || got != "v" {
		t.Fatalf("Get from a fresh instance: found=%v got=%q", found, got)
	}
}

func contains(haystack []byte, needle string) bool {
	return len(needle) > 0 && indexOf(haystack, needle) >= 0
}

func indexOf(haystack []byte, needle string) int {
	n := len(needle)
	for i := 0; i+n <= len(haystack); i++ {
		if string(haystack[i:i+n]) == needle {
			return i
		}
	}
	return -1
}
