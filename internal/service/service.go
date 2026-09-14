// Package service installs nanobotd as something your machine keeps
// running.
//
// Twelve of the sixteen catalog swarms carry a cron trigger — "every
// morning", "every Monday", "every 30 minutes" — and the scheduler that
// fires them works. None of it happens unless `nanobots up` is running,
// which until now meant a terminal window someone remembered to leave open.
// Close the laptop and the morning brief doesn't happen. A product whose
// entire catalog is written in the future tense has to survive a reboot.
//
// Deliberately a command someone runs, never something that installs
// itself. Writing into a user's LaunchAgents is exactly the kind of thing
// that should be asked for out loud, and `nanobots service status` says
// what is there before `install` puts anything.
package service

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Label is the launchd job name, and the plist's filename. Reverse-DNS
// because that is the convention launchd jobs follow and what shows up in
// `launchctl list`.
const Label = "dev.nanobots.nanobotd"

// Config is everything the generated job needs to be reproducible.
type Config struct {
	// Binary is the absolute path to the nanobots executable. Resolved by
	// the caller rather than looked up here: a service pointing at
	// whatever happened to be on $PATH at install time is a service that
	// breaks silently when the binary moves.
	Binary string
	// RepoRoot is the working directory. nanobotd resolves bots/,
	// examples/swarms/ and the harness Dockerfiles relative to it, so a
	// job with the wrong one starts fine and finds no bots.
	RepoRoot string
	// Addr is the address to listen on.
	Addr string
	// LogDir is where stdout and stderr go. A background service with
	// nowhere to write its log is a service nobody can debug.
	LogDir string
}

// Supported reports whether this OS has an implementation.
//
// macOS only, and said plainly rather than by generating a systemd unit
// that has never been run. Linux support is a small amount of work and
// belongs to someone who can test it.
func Supported() bool { return runtime.GOOS == "darwin" }

// PlistPath is where the LaunchAgent goes: per-user, not system-wide.
// nanobotd holds one person's credentials and runs their automations; it
// has no business in /Library/LaunchDaemons running as root.
func PlistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist")
}

// plist is the launchd job description.
//
// Generated through encoding/xml rather than a format string so a path
// containing an ampersand or a quote produces a valid file rather than a
// job that silently never loads.
type plist struct {
	XMLName xml.Name `xml:"plist"`
	Version string   `xml:"version,attr"`
	Dict    dict     `xml:"dict"`
}

type dict struct {
	Entries []any
}

// MarshalXML writes the alternating <key>/<value> pairs launchd expects.
// A plist dict is not a Go map — order is part of the format, and encoding
// a map would produce a different file every run.
func (d dict) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if err := e.EncodeToken(xml.StartElement{Name: xml.Name{Local: "dict"}}); err != nil {
		return err
	}
	for i := 0; i < len(d.Entries); i += 2 {
		key, _ := d.Entries[i].(string)
		if err := e.EncodeElement(key, xml.StartElement{Name: xml.Name{Local: "key"}}); err != nil {
			return err
		}
		switch v := d.Entries[i+1].(type) {
		case string:
			if err := e.EncodeElement(v, xml.StartElement{Name: xml.Name{Local: "string"}}); err != nil {
				return err
			}
		case bool:
			name := "false"
			if v {
				name = "true"
			}
			if err := e.EncodeToken(xml.StartElement{Name: xml.Name{Local: name}}); err != nil {
				return err
			}
			if err := e.EncodeToken(xml.EndElement{Name: xml.Name{Local: name}}); err != nil {
				return err
			}
		case []string:
			if err := e.EncodeToken(xml.StartElement{Name: xml.Name{Local: "array"}}); err != nil {
				return err
			}
			for _, s := range v {
				if err := e.EncodeElement(s, xml.StartElement{Name: xml.Name{Local: "string"}}); err != nil {
					return err
				}
			}
			if err := e.EncodeToken(xml.EndElement{Name: xml.Name{Local: "array"}}); err != nil {
				return err
			}
		}
	}
	return e.EncodeToken(xml.EndElement{Name: xml.Name{Local: "dict"}})
}

// Plist renders the LaunchAgent for cfg.
func Plist(cfg Config) (string, error) {
	if cfg.Binary == "" || cfg.RepoRoot == "" {
		return "", fmt.Errorf("service: need both a binary and a repo root")
	}
	p := plist{
		Version: "1.0",
		Dict: dict{Entries: []any{
			"Label", Label,
			"ProgramArguments", []string{cfg.Binary, "up", "--addr", cfg.Addr},
			// nanobotd resolves bots/ and examples/swarms/ relative to
			// this, so it is not optional decoration.
			"WorkingDirectory", cfg.RepoRoot,
			// At login, and kept alive: a scheduler that stops on the
			// first crash is a scheduler nobody can rely on, and "every
			// morning" has to survive a bad night.
			"RunAtLoad", true,
			"KeepAlive", true,
			"StandardOutPath", filepath.Join(cfg.LogDir, "nanobotd.log"),
			"StandardErrorPath", filepath.Join(cfg.LogDir, "nanobotd.err.log"),
		}},
	}
	body, err := xml.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}
	return xml.Header +
		"<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" " +
		"\"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n" +
		string(body) + "\n", nil
}

// Write puts the plist on disk, creating the LaunchAgents directory if this
// is the first one. Returns the path it wrote.
func Write(home string, cfg Config) (string, error) {
	if !Supported() {
		return "", fmt.Errorf("service install is macOS-only in this build (this is %s) — "+
			"run `nanobots up` under your own supervisor, or add a unit for your init system", runtime.GOOS)
	}
	body, err := Plist(cfg)
	if err != nil {
		return "", err
	}
	path := PlistPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(cfg.LogDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Installed reports whether a job file exists, and where.
func Installed(home string) (string, bool) {
	path := PlistPath(home)
	if _, err := os.Stat(path); err != nil {
		return path, false
	}
	return path, true
}

// Remove deletes the job file. Unloading it is the caller's job — that is a
// launchctl call, and this package only writes files.
func Remove(home string) (string, error) {
	path := PlistPath(home)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return path, err
	}
	return path, nil
}

// DescribeMissedRuns is the one behaviour worth saying out loud when
// someone installs this.
//
// A trigger that came due while nanobotd was off does not fire on startup:
// the scheduler computes each schedule's next occurrence forward from now.
// That is deliberate — booting a laptop after a week away should not
// unleash seven mornings of email at once — but it is not what everyone
// assumes, and assuming wrong here means quietly missing a run.
const DescribeMissedRuns = "A trigger that came due while this was not running does not fire late. " +
	"Schedules resume from now, so a laptop opened after a week away sends one morning brief, not seven."

// LaunchctlHint returns the commands to load and unload the job, for a
// message rather than for execution — this package writes a file and tells
// you what it did; it does not reach for launchctl behind your back.
func LaunchctlHint(path string) (load, unload string) {
	q := func(s string) string {
		if strings.ContainsAny(s, " \t") {
			return "'" + s + "'"
		}
		return s
	}
	return "launchctl load -w " + q(path), "launchctl unload -w " + q(path)
}
