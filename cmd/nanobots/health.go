package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// `nanobots health` — is the daemon answering?
//
// This exists because of the container. The image is distroless: the whole
// filesystem is the binary, the catalog and the example swarms, with no
// shell, no curl and no wget. A compose healthcheck therefore has exactly
// one executable available to it, and `nanobots version` is not a health
// check — it prints a string compiled into the binary and would report a
// dead daemon as healthy, which is the kind of lie this repo goes out of its
// way not to tell.
//
// So: a real GET of /api/status, the same endpoint the UI's status bar
// polls, with an exit code a healthcheck can read.

const healthUsage = `usage: nanobots health [--addr host:port] [--quiet]

  --addr <host:port>  where the daemon is (default 127.0.0.1:7474)
  --quiet             exit code only, no output

Exits 0 when the daemon answers, 1 when it does not.`

func runHealth(args []string) error {
	addr := "127.0.0.1:7474"
	quiet := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--addr":
			i++
			if i >= len(args) {
				return fmt.Errorf("--addr requires a host:port")
			}
			addr = args[i]
		case "--quiet", "-q":
			quiet = true
		case "-h", "--help":
			fmt.Println(healthUsage)
			return nil
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}

	out := io.Writer(os.Stdout)
	if quiet {
		out = io.Discard
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + addr + "/api/status")
	if err != nil {
		return fmt.Errorf("no answer from %s: %w\nIs `nanobots up` running?", addr, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d: %s", addr, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Report what it said, not just that it said something. A daemon with no
	// model configured is healthy and worth knowing about — that is the
	// difference between "broken" and "answering from example data", and it
	// is the first thing anyone debugging a container asks.
	var status struct {
		LLMBackend string `json:"llm_backend"`
		OneClaw    bool   `json:"oneclaw_configured"`
		Docker     bool   `json:"docker_available"`
	}
	_ = json.Unmarshal(body, &status)
	fmt.Fprintf(out, "ok  %s", addr)
	if status.LLMBackend != "" {
		fmt.Fprintf(out, "  model: %s", status.LLMBackend)
	}
	if !status.OneClaw {
		fmt.Fprint(out, "  1claw: not configured")
	}
	if !status.Docker {
		fmt.Fprint(out, "  docker: unavailable (the 5 browser bots cannot run)")
	}
	fmt.Fprintln(out)
	return nil
}
