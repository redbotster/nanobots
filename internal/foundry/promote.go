package foundry

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/redbotster/nanobots/internal/contract"
)

// promote copies a conformance-passing bot out of its sandbox worktree and
// into the real catalog — a plain file write, no git add/commit into the
// user's own history, matching handleSaveSwarm's existing behavior exactly
// for a saved swarm. Only ever called after a human has approved the job.
func promote(worktreeDir, botsDir, botID string) error {
	dest := filepath.Join(botsDir, botID)
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("bots/%s already exists — refusing to overwrite", botID)
	} else if !os.IsNotExist(err) {
		return err
	}

	src := filepath.Join(worktreeDir, "bots", botID)
	if err := copyDir(src, dest); err != nil {
		return fmt.Errorf("copy %s into the catalog: %w", botID, err)
	}

	// Never trust the agent's own self-report: re-run conformance against
	// the real, promoted location — the same instinct the planner already
	// applies by independently re-validating the composer's own draft.
	report, err := contract.RunConformance(dest, "")
	if err != nil || !report.OK() {
		os.RemoveAll(dest)
		if err != nil {
			return fmt.Errorf("re-conformance failed after promotion, rolled back: %w", err)
		}
		return fmt.Errorf("re-conformance failed after promotion, rolled back:\n%s", report.String())
	}
	return nil
}

// copyDir recursively copies src into dest (dest is created; src must be a
// directory). Plain filepath.WalkDir + os.WriteFile — no shelling out.
func copyDir(src, dest string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
