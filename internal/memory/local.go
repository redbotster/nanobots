package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Local is file-backed key/value memory under a directory this daemon owns.
//
// It exists so memory works with nothing configured at all. Before this,
// `memory.get` and `memory.put` only did anything when 1Claw was set up,
// which meant the two bots that use memory quietly behaved differently
// depending on a credential unrelated to what they were remembering —
// competitor-watch would report everything as new on every run.
//
// One file per namespace, so a namespace can be read, diffed or deleted
// with ordinary tools; the whole store is small and written rarely.
type Local struct {
	Dir string

	mu sync.Mutex
}

func NewLocal(dir string) (*Local, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create memory dir: %w", err)
	}
	return &Local{Dir: dir}, nil
}

// namespaceFile keeps the readable name where it's safe and hashes it when
// it isn't, so a namespace containing a slash or ".." can't escape the
// directory while an ordinary one stays greppable.
func (l *Local) namespaceFile(namespace string) string {
	safe := true
	for _, r := range namespace {
		if !(r == '-' || r == '_' || r == '.' && false ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			safe = false
			break
		}
	}
	if safe && namespace != "" {
		return filepath.Join(l.Dir, namespace+".json")
	}
	sum := sha256.Sum256([]byte(namespace))
	return filepath.Join(l.Dir, hex.EncodeToString(sum[:])+".json")
}

func (l *Local) read(namespace string) (map[string]string, error) {
	out := map[string]string{}
	raw, err := os.ReadFile(l.namespaceFile(namespace))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("memory namespace %q is unreadable: %w", namespace, err)
	}
	return out, nil
}

func (l *Local) Get(_ context.Context, namespace, key string) (string, bool, error) {
	if err := validKey(namespace, key); err != nil {
		return "", false, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	m, err := l.read(namespace)
	if err != nil {
		return "", false, err
	}
	v, ok := m[key]
	return v, ok, nil
}

func (l *Local) Put(_ context.Context, namespace, key, value string) error {
	if err := validKey(namespace, key); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	m, err := l.read(namespace)
	if err != nil {
		return err
	}
	m[key] = value
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	// Temp-then-rename: a half-written namespace file would lose every key
	// in it, not just the one being set.
	path := l.namespaceFile(namespace)
	tmp, err := os.CreateTemp(l.Dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

var _ Store = (*Local)(nil)
