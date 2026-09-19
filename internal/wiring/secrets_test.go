package wiring

import (
	"strings"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/secrets"
)

// This is the regression test for a real incident, not a hypothetical one:
// the original default tried secrets.Available() (a real OS keychain
// probe) whenever no backend was chosen and no 1Claw was configured. Run
// inside this repo's own `go test ./... -race`, that probe hit a session
// where the underlying `security` call doesn't fail fast — it can pop a
// real permission dialog and then block waiting for a human to click it —
// and internal/daemon's tests hung for the full ten-minute go test
// timeout. nanobotd's own unattended startup would have hung exactly the
// same way. The fix is structural, not just a timeout: the default no
// longer touches the keychain at all. A generous deadline here still
// proves it, so a future regression that reintroduces the probe fails this
// test in seconds rather than reintroducing a ten-minute hang.
func TestBuildSecretsStoreDefaultsToFileWithoutProbingTheKeychain(t *testing.T) {
	done := make(chan secrets.Store, 1)
	errCh := make(chan error, 1)
	go func() {
		store, err := BuildSecretsStore(nil, t.TempDir(), nil)
		if err != nil {
			errCh <- err
			return
		}
		done <- store
	}()

	select {
	case err := <-errCh:
		t.Fatalf("BuildSecretsStore: %v", err)
	case store := <-done:
		if _, ok := store.(*secrets.File); !ok {
			t.Fatalf("default backend = %T, want *secrets.File", store)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("BuildSecretsStore did not return within 5s — it must never probe the OS keychain by default")
	}
}

func TestBuildSecretsStoreExplicitFile(t *testing.T) {
	t.Setenv(secretsBackendEnv, "file")
	dir := t.TempDir()
	store, err := BuildSecretsStore(nil, dir, nil)
	if err != nil {
		t.Fatalf("BuildSecretsStore: %v", err)
	}
	f, ok := store.(*secrets.File)
	if !ok || f.Dir != dir {
		t.Fatalf("store = %#v, want *secrets.File{Dir: %q}", store, dir)
	}
}

func TestBuildSecretsStoreExplicitKeychain(t *testing.T) {
	t.Setenv(secretsBackendEnv, "keychain")
	store, err := BuildSecretsStore(nil, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("BuildSecretsStore: %v", err)
	}
	if _, ok := store.(secrets.Keychain); !ok {
		t.Fatalf("store = %#v, want secrets.Keychain", store)
	}
}

func TestBuildSecretsStoreExplicitOneClawWithoutOneClawConfiguredFails(t *testing.T) {
	t.Setenv(secretsBackendEnv, "oneclaw")
	if _, err := BuildSecretsStore(nil, t.TempDir(), nil); err == nil {
		t.Fatal("expected an error requesting the oneclaw backend with no 1Claw configured")
	}
}

func TestBuildSecretsStoreRejectsAnUnrecognizedBackend(t *testing.T) {
	t.Setenv(secretsBackendEnv, "dropbox")
	_, err := BuildSecretsStore(nil, t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected an error for an unrecognized backend name")
	}
	if !strings.Contains(err.Error(), "oneclaw") || !strings.Contains(err.Error(), "keychain") || !strings.Contains(err.Error(), "file") {
		t.Errorf("error should name the valid choices: %v", err)
	}
}
