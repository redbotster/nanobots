package oneclaw

import (
	"os"
	"path/filepath"
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
