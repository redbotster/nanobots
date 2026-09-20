package team

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreferencesFallsBackToTheDaemonsResolvedDefaultWhenNoFileExistsYet(t *testing.T) {
	p, err := NewPreferences(filepath.Join(t.TempDir(), "team-engines.json"), EngineGemini)
	if err != nil {
		t.Fatalf("NewPreferences: %v", err)
	}
	if p.Default() != EngineGemini {
		t.Errorf("Default() = %q, want %q", p.Default(), EngineGemini)
	}
	if got := p.EngineFor("designer"); got != EngineGemini {
		t.Errorf("EngineFor(designer) = %q, want the default %q", got, EngineGemini)
	}
}

func TestSetDefaultPersistsAndSurvivesAReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "team-engines.json")
	p, err := NewPreferences(path, EngineGemini)
	if err != nil {
		t.Fatalf("NewPreferences: %v", err)
	}
	if err := p.SetDefault(EngineClaude); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	if p.Default() != EngineClaude {
		t.Errorf("Default() = %q, want %q", p.Default(), EngineClaude)
	}

	// A fresh load from disk — the daemon reloading, not just this instance
	// — must see the change too, not just the process that wrote it.
	reloaded, err := NewPreferences(path, EngineGemini)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Default() != EngineClaude {
		t.Errorf("reloaded Default() = %q, want %q (the fallback %q must not win over a saved choice)",
			reloaded.Default(), EngineClaude, EngineGemini)
	}
}

func TestSetDefaultRejectsAnUnknownEngine(t *testing.T) {
	p, _ := NewPreferences(filepath.Join(t.TempDir(), "team-engines.json"), EngineClaude)
	if err := p.SetDefault("chatgpt"); err == nil {
		t.Error("expected an error for an unknown engine")
	}
	if p.Default() != EngineClaude {
		t.Errorf("a rejected write must not change the live value: Default() = %q", p.Default())
	}
}

func TestRoleOverrideWinsOverTheDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "team-engines.json")
	p, _ := NewPreferences(path, EngineClaude)
	if err := p.SetRoleEngine("designer", EngineGemini); err != nil {
		t.Fatalf("SetRoleEngine: %v", err)
	}

	if got := p.EngineFor("designer"); got != EngineGemini {
		t.Errorf("EngineFor(designer) = %q, want its override %q", got, EngineGemini)
	}
	// A different role, never given an override, still gets the default.
	if got := p.EngineFor("backend-engineer"); got != EngineClaude {
		t.Errorf("EngineFor(backend-engineer) = %q, want the default %q", got, EngineClaude)
	}

	overrides := p.RoleOverrides()
	if overrides["designer"] != EngineGemini || len(overrides) != 1 {
		t.Errorf("RoleOverrides() = %v, want exactly {designer: gemini}", overrides)
	}
}

func TestClearingARoleOverrideFallsBackToTheDefault(t *testing.T) {
	p, _ := NewPreferences(filepath.Join(t.TempDir(), "team-engines.json"), EngineClaude)
	if err := p.SetRoleEngine("designer", EngineGemini); err != nil {
		t.Fatalf("SetRoleEngine: %v", err)
	}
	if err := p.SetRoleEngine("designer", ""); err != nil {
		t.Fatalf("clear SetRoleEngine: %v", err)
	}
	if got := p.EngineFor("designer"); got != EngineClaude {
		t.Errorf("EngineFor(designer) after clearing = %q, want the default %q", got, EngineClaude)
	}
	if len(p.RoleOverrides()) != 0 {
		t.Errorf("RoleOverrides() = %v, want none left after clearing the only one", p.RoleOverrides())
	}
}

func TestSetRoleEngineRejectsAnUnknownEngine(t *testing.T) {
	p, _ := NewPreferences(filepath.Join(t.TempDir(), "team-engines.json"), EngineClaude)
	if err := p.SetRoleEngine("designer", "chatgpt"); err == nil {
		t.Error("expected an error for an unknown engine")
	}
	if got := p.EngineFor("designer"); got != EngineClaude {
		t.Errorf("a rejected write must not change the live value: EngineFor(designer) = %q", got)
	}
}

func TestACorruptFileStartsCleanRatherThanFailingTheWholeDaemon(t *testing.T) {
	path := filepath.Join(t.TempDir(), "team-engines.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := NewPreferences(path, EngineGemini)
	if err != nil {
		t.Fatalf("NewPreferences should recover from a corrupt file, not fail: %v", err)
	}
	if p.Default() != EngineGemini {
		t.Errorf("Default() = %q, want the fallback %q", p.Default(), EngineGemini)
	}
}
