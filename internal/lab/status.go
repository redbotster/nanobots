package lab

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/redbotster/nanobots/internal/team"
)

// reportStatus answers Lab's second tool — "read what a Team agent is
// doing" — from the one source of truth a persistent workspace already
// has: its own commit history. No new state to keep in sync with the
// workspace itself, and no way for it to lie about what happened, since
// it's reading exactly what the role's own agent committed.
func (s *Session) reportStatus(role string) string {
	workDir := filepath.Join(s.cfg.Team.TeamDir, role, "workspace")
	cmd := exec.Command("git", "-C", workDir, "log", "--oneline", "-n", "10")
	out, err := cmd.Output()
	if err != nil {
		roles, _ := team.Roles(s.cfg.Team.TeamDir)
		if len(roles) == 0 {
			return fmt.Sprintf("%s has no workspace yet — no task has ever been delegated to them.", role)
		}
		return fmt.Sprintf("%s has no workspace yet. Existing roles: %s.", role, strings.Join(roles, ", "))
	}
	log := strings.TrimSpace(string(out))
	if log == "" {
		return fmt.Sprintf("%s's workspace exists but has no commits yet.", role)
	}
	return fmt.Sprintf("%s's last commits:\n%s", role, log)
}
