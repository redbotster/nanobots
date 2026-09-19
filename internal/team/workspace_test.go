package team

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitRepo builds a minimal real git repo (a commit, so HEAD resolves) to
// run worktree operations against, without touching the real nanobots repo.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("probe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-q", "-m", "initial")
	return dir
}

func TestEnsureWorkspaceCreatesAWorktreeOnItsOwnBranch(t *testing.T) {
	repo := gitRepo(t)
	teamDir := t.TempDir()

	workDir, err := EnsureWorkspace(repo, teamDir, "backend-engineer")
	if err != nil {
		t.Fatalf("EnsureWorkspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "README.md")); err != nil {
		t.Errorf("workspace missing the repo's own files: %v", err)
	}

	branch := exec.Command("git", "-C", workDir, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := branch.Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	if got := string(out); got != "team/backend-engineer\n" {
		t.Errorf("branch = %q, want team/backend-engineer", got)
	}
}

// The whole point of a persistent workspace: a second task for the same
// role gets the same directory back, not a fresh one — so uncommitted
// work and git history from the first task are still there.
func TestEnsureWorkspaceIsIdempotent(t *testing.T) {
	repo := gitRepo(t)
	teamDir := t.TempDir()

	first, err := EnsureWorkspace(repo, teamDir, "designer")
	if err != nil {
		t.Fatalf("first EnsureWorkspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(first, "left-behind.txt"), []byte("from task 1"), 0o600); err != nil {
		t.Fatal(err)
	}

	second, err := EnsureWorkspace(repo, teamDir, "designer")
	if err != nil {
		t.Fatalf("second EnsureWorkspace: %v", err)
	}
	if first != second {
		t.Fatalf("workspace paths differ: %q vs %q", first, second)
	}
	if _, err := os.Stat(filepath.Join(second, "left-behind.txt")); err != nil {
		t.Errorf("the first task's file is gone from the reused workspace: %v", err)
	}
}

// Two roles get two independent workspaces, on two branches.
func TestEnsureWorkspaceIsolatesRoles(t *testing.T) {
	repo := gitRepo(t)
	teamDir := t.TempDir()

	a, err := EnsureWorkspace(repo, teamDir, "backend-engineer")
	if err != nil {
		t.Fatal(err)
	}
	b, err := EnsureWorkspace(repo, teamDir, "designer")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("two different roles got the same workspace: %q", a)
	}
	if err := os.WriteFile(filepath.Join(a, "backend-only.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b, "backend-only.txt")); err == nil {
		t.Error("designer's workspace sees a file only written to backend-engineer's")
	}
}

// A branch that survives a worktree directory removed by hand (someone
// cleaning up disk space, or a partial prior run) is reused rather than
// starting the role over from HEAD — the workspace's history lives in the
// branch, not the directory.
func TestEnsureWorkspaceReusesABranchWhoseWorktreeDirWasRemoved(t *testing.T) {
	repo := gitRepo(t)
	teamDir := t.TempDir()

	workDir, err := EnsureWorkspace(repo, teamDir, "designer")
	if err != nil {
		t.Fatal(err)
	}
	commit := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", workDir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(workDir, "marker.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	commit("add", "marker.txt")
	commit("commit", "-q", "-m", "designer's work")

	// Remove just the worktree directory, the way tidying disk space would
	// — not `git worktree remove`, which would also drop the branch's
	// worktree registration in a way this is specifically testing around.
	if err := exec.Command("git", "-C", repo, "worktree", "remove", "--force", workDir).Run(); err != nil {
		t.Fatalf("git worktree remove: %v", err)
	}

	again, err := EnsureWorkspace(repo, teamDir, "designer")
	if err != nil {
		t.Fatalf("EnsureWorkspace after the dir was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(again, "marker.txt")); err != nil {
		t.Errorf("the branch's commit didn't survive: %v", err)
	}
}

func TestRolesListsEveryWorkspaceCreated(t *testing.T) {
	repo := gitRepo(t)
	teamDir := t.TempDir()

	if _, err := EnsureWorkspace(repo, teamDir, "backend-engineer"); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureWorkspace(repo, teamDir, "designer"); err != nil {
		t.Fatal(err)
	}

	roles, err := Roles(teamDir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range roles {
		got[r] = true
	}
	if !got["backend-engineer"] || !got["designer"] || len(got) != 2 {
		t.Errorf("Roles() = %v, want exactly [backend-engineer designer]", roles)
	}
}

func TestRolesOnAFreshTeamDirIsEmpty(t *testing.T) {
	roles, err := Roles(filepath.Join(t.TempDir(), "does-not-exist-yet"))
	if err != nil {
		t.Fatalf("Roles: %v", err)
	}
	if len(roles) != 0 {
		t.Errorf("Roles() = %v, want none", roles)
	}
}
