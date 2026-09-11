package oneclaw

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAPIKeyParsesDotenv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nanobots.env")
	content := "# comment\nSOME_OTHER_VAR=1\nONECLAW_API_KEY=\"1ck_abc123\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := LoadAPIKey(path)
	if err != nil {
		t.Fatalf("LoadAPIKey: %v", err)
	}
	if key != "1ck_abc123" {
		t.Errorf("key = %q, want 1ck_abc123", key)
	}
}

func TestLoadAPIKeyMissingFileReturnsEmpty(t *testing.T) {
	key, err := LoadAPIKey(filepath.Join(t.TempDir(), "does-not-exist.env"))
	if err != nil {
		t.Fatalf("LoadAPIKey: %v", err)
	}
	if key != "" {
		t.Errorf("key = %q, want empty for a missing file", key)
	}
}

func TestLoadEnvValueReadsAnyKeyFromTheSameFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nanobots.env")
	content := "ONECLAW_API_KEY=1ck_abc123\nGOOGLE_OAUTH_CLIENT_ID=123-abc.apps.googleusercontent.com\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadEnvValue(path, "GOOGLE_OAUTH_CLIENT_ID")
	if err != nil {
		t.Fatalf("LoadEnvValue: %v", err)
	}
	if got != "123-abc.apps.googleusercontent.com" {
		t.Errorf("got = %q", got)
	}
}

func TestWriteEnvValueCreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "nanobots.env")
	if err := WriteEnvValue(path, "ONECLAW_API_KEY", "1ck_new"); err != nil {
		t.Fatalf("WriteEnvValue: %v", err)
	}
	got, err := LoadEnvValue(path, "ONECLAW_API_KEY")
	if err != nil {
		t.Fatalf("LoadEnvValue: %v", err)
	}
	if got != "1ck_new" {
		t.Errorf("got = %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestWriteEnvValueReplacesExistingKeyPreservingEverythingElse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nanobots.env")
	original := "# a comment\nGOOGLE_OAUTH_CLIENT_ID=abc\nONECLAW_API_KEY=1ck_old\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteEnvValue(path, "ONECLAW_API_KEY", "1ck_new"); err != nil {
		t.Fatalf("WriteEnvValue: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if !strings.Contains(got, "# a comment") || !strings.Contains(got, "GOOGLE_OAUTH_CLIENT_ID=abc") {
		t.Errorf("expected unrelated lines preserved, got:\n%s", got)
	}
	if strings.Contains(got, "1ck_old") {
		t.Errorf("expected the old value gone, got:\n%s", got)
	}
	key, err := LoadEnvValue(path, "ONECLAW_API_KEY")
	if err != nil || key != "1ck_new" {
		t.Errorf("LoadEnvValue after write = %q, %v", key, err)
	}
}

func TestWriteEnvValueAppendsWhenKeyAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nanobots.env")
	if err := os.WriteFile(path, []byte("GOOGLE_OAUTH_CLIENT_ID=abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteEnvValue(path, "ONECLAW_API_KEY", "1ck_new"); err != nil {
		t.Fatalf("WriteEnvValue: %v", err)
	}
	got, err := LoadEnvValue(path, "GOOGLE_OAUTH_CLIENT_ID")
	if err != nil || got != "abc" {
		t.Errorf("existing key survived = %q, %v", got, err)
	}
	got, err = LoadEnvValue(path, "ONECLAW_API_KEY")
	if err != nil || got != "1ck_new" {
		t.Errorf("appended key = %q, %v", got, err)
	}
}

func TestAgentCredentialRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := saveAgentCredential(dir, "bot-a", agentCredential{AgentID: "a1", APIKey: "ocv_x"}); err != nil {
		t.Fatalf("saveAgentCredential: %v", err)
	}
	cred, ok, err := loadAgentCredential(dir, "bot-a")
	if err != nil || !ok {
		t.Fatalf("loadAgentCredential: %v, ok=%v", err, ok)
	}
	if cred.AgentID != "a1" || cred.APIKey != "ocv_x" {
		t.Errorf("loaded credential = %+v", cred)
	}
	if _, ok, _ := loadAgentCredential(dir, "bot-b"); ok {
		t.Error("expected no credential for an unsaved bot name")
	}
}
