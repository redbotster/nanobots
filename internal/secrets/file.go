package secrets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"filippo.io/age"
)

// File is a local, encrypted-at-rest secrets store — no 1Claw account, no
// OS keychain, nothing but this machine's own filesystem.
//
// What it actually protects against, stated plainly rather than implied:
// the identity (the age private key that decrypts everything) lives right
// next to the ciphertext, both at mode 0600 — the same trust boundary
// ~/.secrets/nanobots.env already has for the 1Claw key itself
// (docs/setup.md). Encrypting the secrets file adds real protection
// against it ending up somewhere with looser permissions by accident (a
// backup, a synced folder, a `tar` that didn't preserve modes) or being
// read by something that can see file contents but not file permissions
// metadata — it does not protect against another process running as the
// same user, which could read the key file just as easily as nanobotd
// does. A real passphrase-protected key (age supports one directly, via a
// scrypt recipient) would close that gap and is deliberately not built
// yet: it needs an interactive prompt somewhere in nanobots init or
// nanobotd's own startup, which doesn't exist for this backend today.
type File struct {
	// Dir holds two files: secrets.age-key (the identity, generated once)
	// and secrets.age (the encrypted store). Both created at 0600 the
	// first time this backend is used.
	Dir string

	mu sync.Mutex
}

const (
	keyFileName   = "secrets.age-key"
	storeFileName = "secrets.age"
)

func (f *File) Describe() string {
	return "a local file, encrypted at rest — protected by this machine's own file permissions, " +
		"the same trust boundary ~/.secrets/nanobots.env already has (see docs/secrets.md)"
}

// identity loads the existing age identity, generating and persisting one
// the first time this backend is used on this machine. Every later call —
// this run, the next one, from the CLI or the daemon — reads the same
// file, so a secret written once stays readable.
func (f *File) identity() (*age.X25519Identity, error) {
	path := filepath.Join(f.Dir, keyFileName)
	raw, err := os.ReadFile(path)
	if err == nil {
		id, err := age.ParseX25519Identity(string(bytes.TrimSpace(raw)))
		if err != nil {
			return nil, fmt.Errorf("secrets: %s is not a valid age identity: %w", path, err)
		}
		return id, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("secrets: read %s: %w", path, err)
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("secrets: generate identity: %w", err)
	}
	if err := os.MkdirAll(f.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("secrets: create %s: %w", f.Dir, err)
	}
	if err := os.WriteFile(path, []byte(id.String()+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("secrets: write %s: %w", path, err)
	}
	return id, nil
}

// load decrypts the store into a map, or returns an empty one if nothing
// has been written yet — a fresh install with no secrets is not an error.
func (f *File) load(id *age.X25519Identity) (map[string]string, error) {
	path := filepath.Join(f.Dir, storeFileName)
	enc, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("secrets: open %s: %w", path, err)
	}
	defer enc.Close()
	r, err := age.Decrypt(enc, id)
	if err != nil {
		return nil, fmt.Errorf("secrets: decrypt %s: %w", path, err)
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("secrets: read decrypted %s: %w", path, err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("secrets: parse %s: %w", path, err)
	}
	return m, nil
}

func (f *File) save(id *age.X25519Identity, m map[string]string) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, id.Recipient())
	if err != nil {
		return fmt.Errorf("secrets: encrypt: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("secrets: encrypt: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("secrets: encrypt: %w", err)
	}
	// Written to a temp file and renamed into place: a torn write on a
	// crash mid-save must not leave a half-written ciphertext that
	// decrypts to garbage or nothing.
	path := filepath.Join(f.Dir, storeFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("secrets: write %s: %w", tmp, err)
	}
	return os.Rename(tmp, path)
}

func (f *File) Get(key string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, err := f.identity()
	if err != nil {
		return "", false, err
	}
	m, err := f.load(id)
	if err != nil {
		return "", false, err
	}
	v, ok := m[key]
	return v, ok, nil
}

func (f *File) Put(key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, err := f.identity()
	if err != nil {
		return err
	}
	m, err := f.load(id)
	if err != nil {
		return err
	}
	m[key] = value
	return f.save(id, m)
}

func (f *File) Delete(key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, err := f.identity()
	if err != nil {
		return err
	}
	m, err := f.load(id)
	if err != nil {
		return err
	}
	delete(m, key)
	return f.save(id, m)
}

var _ Store = (*File)(nil)
