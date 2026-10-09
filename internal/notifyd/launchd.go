package notifyd

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// Label is notifyd's name to launchd.
const Label = "com.github.0xdeafcafe.loafer.notifyd"

// Runner runs launchctl with args. Tests swap it, so they never run it.
type Runner func(args ...string) error

// Launchctl is the Runner that runs it, with args and no shell.
func Launchctl(args ...string) error {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %w: %s", args[0], err, bytes.TrimSpace(out))
	}
	return nil
}

// Agent is notifyd's LaunchAgent.
type Agent struct {
	Dir string // ~/Library/LaunchAgents
	Run Runner
}

// Path is the plist's.
func (a Agent) Path() string { return filepath.Join(a.Dir, Label+".plist") }

func domain() string  { return "gui/" + strconv.Itoa(os.Getuid()) }
func service() string { return domain() + "/" + Label }

// Install writes the plist to run exe notifyd at login, and loads it.
// Again, it reloads it if the plist changed, or restarts it if not, so a
// new build takes over.
func (a Agent) Install(exe string) error {
	want := plist(exe)
	if err := os.MkdirAll(a.Dir, 0o755); err != nil {
		return err
	}
	had, _ := os.ReadFile(a.Path())
	loaded := a.Loaded()
	if loaded && bytes.Equal(had, want) {
		return a.Run("kickstart", "-k", service())
	}
	if err := os.WriteFile(a.Path(), want, 0o644); err != nil {
		return err
	}
	if loaded {
		if err := a.Run("bootout", service()); err != nil {
			return err
		}
	}
	return a.Run("bootstrap", domain(), a.Path())
}

// Uninstall unloads notifyd and removes its plist; with neither there, it
// does nothing.
func (a Agent) Uninstall() error {
	if a.Loaded() {
		if err := a.Run("bootout", service()); err != nil {
			return err
		}
	}
	if err := os.Remove(a.Path()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Loaded says whether launchd has notifyd.
func (a Agent) Loaded() bool { return a.Run("print", service()) == nil }

// Installed says whether the plist is there.
func (a Agent) Installed() bool {
	_, err := os.Stat(a.Path())
	return err == nil
}

// plist runs exe notifyd at login, and again if it fails. A clean exit
// (another notifyd has it) stays down.
func plist(exe string) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + Label + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>`)
	_ = xml.EscapeText(&b, []byte(exe))
	b.WriteString(`</string>
		<string>notifyd</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
</dict>
</plist>
`)
	return b.Bytes()
}
