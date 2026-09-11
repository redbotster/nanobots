package google

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadClientIDReadsFromTheSharedEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nanobots.env")
	content := "ONECLAW_API_KEY=1ck_x\nGOOGLE_OAUTH_CLIENT_ID=123-abc.apps.googleusercontent.com\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadClientID(path)
	if err != nil {
		t.Fatalf("LoadClientID: %v", err)
	}
	if got != "123-abc.apps.googleusercontent.com" {
		t.Errorf("got = %q", got)
	}
}

func TestLoadClientIDMissingFileReturnsEmpty(t *testing.T) {
	got, err := LoadClientID(filepath.Join(t.TempDir(), "does-not-exist.env"))
	if err != nil {
		t.Fatalf("LoadClientID: %v", err)
	}
	if got != "" {
		t.Errorf("got = %q, want empty", got)
	}
}
