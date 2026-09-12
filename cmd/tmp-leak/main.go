package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/redbotster/nanobots/internal/runner"
)

func main() {
	// A bot that ignores its budget: sleeps far past MaxRuntime.
	_, _, err := runner.RunContainer(runner.ContainerSpec{
		Image:      "nanobots-hang-probe:local",
		User:       "0:0",
		BotDir:     "/tmp",
		RunDir:     "/tmp",
		MaxRuntime: 3 * time.Second,
		Env:        map[string]string{"X": "1"},
	})
	fmt.Println("RunContainer err:", err)

	time.Sleep(2 * time.Second)
	out, _ := exec.Command("docker", "ps", "--format", "{{.Names}} {{.Status}}").Output()
	leaked := []string{}
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(l, "nanobot-") {
			leaked = append(leaked, l)
		}
	}
	if len(leaked) == 0 {
		fmt.Println("RESULT: no orphaned container — the timeout actually stopped it")
	} else {
		fmt.Println("RESULT: LEAKED:", leaked)
	}
}
