package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

// `nanobots init` — the first run, without a text editor.
//
// Setting this up used to mean creating ~/.secrets/nanobots.env by hand and
// knowing that ONECLAW_API_KEY was the line to put in it. That is a fine
// instruction for someone who has already read the README twice and a wall
// for everyone else, and it is the step between "I heard about this" and
// "it does something".
//
// Three things this deliberately does not do:
//
// It does not require 1Claw. Every bot ships on `connection: demo` and
// answers from this repo's fixtures, so skipping every question still
// leaves a working install — that path stays one keypress away rather than
// being buried as a fallback.
//
// It does not ask for a Human API key when an agent key will do. See
// keyKindHelp: an agent key is narrower, and narrower is the right default
// for a program that runs unattended on your laptop.
//
// It does not pretend enrolment is hands-free. 1Claw returns a new agent's
// key once, to the human approving in the browser, and there is no endpoint
// for this process to collect it afterwards — so the honest flow is "open
// this URL, approve, paste what it shows you", and that is what it says.

const keyKindHelp = `
Two kinds of 1Claw key work here, and they are not equivalent:

  agent key (ocv_)  Narrower, and the better default. Reads and writes its
                    own vault, runs bots, and — the one that matters —
                    can ask you to approve something, which is how an
                    overnight swarm reaches your phone.
                    Cannot install connectors, create bindings, or read
                    the org's security posture. The Settings page will say
                    so rather than showing zeroes.

  Human key (1ck_)  Everything, including your whole 1Claw account. Use it
                    if you want connector installs and the posture panel,
                    and know that this file then holds full account access.
`

type initOptions struct {
	EnvPath string
	// In/Out are the prompt's ends, so a test drives this without a
	// terminal and without touching a real home directory.
	In  io.Reader
	Out io.Writer
	// NoBrowser skips the "open it for you" step, which a test always wants
	// and a headless machine usually does.
	NoBrowser bool
}

func runInit(args []string) error {
	opts := initOptions{In: os.Stdin, Out: os.Stdout}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--env":
			i++
			if i >= len(args) {
				return fmt.Errorf("--env requires a path")
			}
			opts.EnvPath = args[i]
		case "--no-browser":
			opts.NoBrowser = true
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	return initSetup(opts)
}

func initSetup(opts initOptions) error {
	out := opts.Out
	in := bufio.NewReader(opts.In)

	envPath := opts.EnvPath
	if envPath == "" {
		p, err := oneclaw.DefaultEnvFilePath()
		if err != nil {
			return err
		}
		envPath = p
	}

	fmt.Fprintln(out, "nanobots setup")
	fmt.Fprintln(out, "==============")
	fmt.Fprintf(out, "\nConfiguration goes in %s (owner-readable only).\n", envPath)

	// Already configured? Say so and leave it alone. Re-running setup should
	// not be how someone loses a working install.
	if existing, _ := oneclaw.LoadEnvValue(envPath, "ONECLAW_API_KEY"); existing != "" {
		fmt.Fprintf(out, "\n1Claw is already configured (%s). Leaving it as it is.\n", maskKey(existing))
	} else {
		if err := setupOneClaw(in, out, envPath, opts.NoBrowser); err != nil {
			return err
		}
	}

	if err := setupModel(in, out, envPath); err != nil {
		return err
	}

	fmt.Fprint(out, "\nDone. Start it with:\n\n    nanobots up\n\n")
	fmt.Fprintln(out, "Then open http://127.0.0.1:7474 and press Run on any swarm.")
	fmt.Fprintln(out, "Everything runs on this repo's example data until you connect an account.")
	return nil
}

func setupOneClaw(in *bufio.Reader, out io.Writer, envPath string, noBrowser bool) error {
	fmt.Fprintln(out, "\n1Claw holds your credentials, bills model calls against a budget,")
	fmt.Fprintln(out, "redacts PII, and delivers approvals. It is optional: without it every")
	fmt.Fprintln(out, "bot runs on example data, which is a real way to use this.")
	fmt.Fprint(out, keyKindHelp)

	fmt.Fprintln(out, "\n  1) Enrol a new agent      (opens a browser, you approve, paste the key)")
	fmt.Fprintln(out, "  2) Paste a key I have     (either kind)")
	fmt.Fprintln(out, "  3) Skip                   (demo data only)")
	choice := ask(in, out, "\nChoice [1/2/3, default 3]: ")

	var key string
	switch choice {
	case "1":
		k, err := enrolAgent(in, out, noBrowser)
		if err != nil {
			fmt.Fprintf(out, "\nEnrolment did not complete: %v\nCarrying on without 1Claw.\n", err)
			return nil
		}
		key = k
	case "2":
		key = ask(in, out, "\nPaste the key (ocv_... or 1ck_...): ")
	default:
		fmt.Fprintln(out, "\nSkipping 1Claw. Every bot will use its fixtures.")
		return nil
	}

	key = strings.TrimSpace(key)
	if key == "" {
		fmt.Fprintln(out, "\nNothing pasted. Carrying on without 1Claw.")
		return nil
	}
	if err := verifyAndWriteKey(out, envPath, key); err != nil {
		fmt.Fprintf(out, "\n%v\nCarrying on without 1Claw; run `nanobots init` again to retry.\n", err)
	}
	return nil
}

// verifyAndWriteKey checks the key actually authenticates before storing it.
//
// A key that is one character short fails at the first run of the first bot,
// which is a long way from where the typo happened. The check picks the
// exchange endpoint from the prefix, because sending an agent key to the
// human endpoint returns a bare 401 that explains nothing.
func verifyAndWriteKey(out io.Writer, envPath, key string) error {
	client := oneclaw.NewClient(key)
	kind := "Human"
	if strings.HasPrefix(key, "ocv_") {
		client = oneclaw.NewAgentClient(key)
		kind = "agent"
	}
	if _, err := client.ListVaults(); err != nil {
		return fmt.Errorf("that key did not authenticate against 1Claw: %w", err)
	}
	if err := oneclaw.WriteEnvValue(envPath, "ONECLAW_API_KEY", key); err != nil {
		return fmt.Errorf("could not write %s: %w", envPath, err)
	}
	fmt.Fprintf(out, "\nSaved a working %s key to %s.\n", kind, envPath)
	if kind == "agent" {
		fmt.Fprintln(out, "Connector installs and the posture panel need a Human key; everything else works.")
	}
	return nil
}

// enrolAgent creates a pending enrolment and waits for the paste.
//
// POST /v1/agents/enroll is public and returns an approval_url. The key is
// handed to whoever approves, in their browser, and no endpoint exists for
// this process to poll for it — so this prints the URL, opens it, and waits.
func enrolAgent(in *bufio.Reader, out io.Writer, noBrowser bool) (string, error) {
	name := ask(in, out, "\nName for this agent [nanobots]: ")
	if name == "" {
		name = "nanobots"
	}
	email := ask(in, out, "Your 1Claw account email (optional, sends you the link): ")

	body := map[string]string{"name": name, "description": "nanobots on " + hostname()}
	if email != "" {
		body["human_email"] = email
	}
	raw, _ := json.Marshal(body)
	resp, err := http.Post(oneclaw.DefaultBaseURL+"/v1/agents/enroll", "application/json", strings.NewReader(string(raw)))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("1Claw answered %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	var enrolled struct {
		ApprovalURL string `json:"approval_url"`
		Message     string `json:"message"`
	}
	_ = json.Unmarshal(payload, &enrolled)
	if enrolled.ApprovalURL == "" {
		return "", fmt.Errorf("1Claw did not return an approval link: %s", strings.TrimSpace(string(payload)))
	}

	fmt.Fprintf(out, "\nApprove this agent here:\n\n    %s\n\n", enrolled.ApprovalURL)
	if email != "" {
		fmt.Fprintln(out, "The same link is in your inbox.")
	}
	fmt.Fprintln(out, "1Claw shows the new key once, on that page. Copy it and paste it below.")
	if !noBrowser {
		openBrowser(enrolled.ApprovalURL)
	}
	return ask(in, out, "\nKey: "), nil
}

func setupModel(in *bufio.Reader, out io.Writer, envPath string) error {
	if k, _ := oneclaw.LoadEnvValue(envPath, "ONECLAW_API_KEY"); k != "" {
		// Shroud is the model backend, and the better one: budget, PII
		// redaction and injection screening come with it.
		return nil
	}
	for _, v := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY"} {
		if k, _ := oneclaw.LoadEnvValue(envPath, v); k != "" {
			fmt.Fprintf(out, "\nUsing %s for model calls.\n", v)
			return nil
		}
	}

	fmt.Fprintln(out, "\nNo 1Claw key, so bots have no model to think with and will return")
	fmt.Fprintln(out, "their example output instead. A provider key fixes that on its own.")
	fmt.Fprintln(out, "\n  1) Anthropic   2) OpenAI   3) Gemini   4) Skip")
	switch ask(in, out, "\nChoice [1-4, default 4]: ") {
	case "1":
		return writeModelKey(in, out, envPath, "ANTHROPIC_API_KEY")
	case "2":
		return writeModelKey(in, out, envPath, "OPENAI_API_KEY")
	case "3":
		return writeModelKey(in, out, envPath, "GEMINI_API_KEY")
	}
	fmt.Fprintln(out, "\nSkipping. Bots will return their example output.")
	return nil
}

func writeModelKey(in *bufio.Reader, out io.Writer, envPath, name string) error {
	v := ask(in, out, "\nPaste "+name+": ")
	if v == "" {
		fmt.Fprintln(out, "Nothing pasted; skipping.")
		return nil
	}
	if err := oneclaw.WriteEnvValue(envPath, name, v); err != nil {
		return err
	}
	fmt.Fprintf(out, "Saved %s to %s.\n", name, envPath)
	return nil
}

func ask(in *bufio.Reader, out io.Writer, prompt string) string {
	fmt.Fprint(out, prompt)
	line, err := in.ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	return strings.TrimSpace(line)
}

// maskKey shows enough to recognise a key and not enough to use it.
func maskKey(k string) string {
	if len(k) <= 10 {
		return "set"
	}
	return k[:6] + "…" + k[len(k)-4:]
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "this machine"
	}
	return h
}

// openBrowser is best-effort and never an error: failing to open a browser
// is not a failed setup, and the URL is always printed first.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch {
	case commandExists("open"):
		cmd = exec.Command("open", url)
	case commandExists("xdg-open"):
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	_ = cmd.Start()
	time.Sleep(200 * time.Millisecond)
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
