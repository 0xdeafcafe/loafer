package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xdeafcafe/loafer/internal/notify"
	"github.com/0xdeafcafe/loafer/internal/notifyd"
	"github.com/0xdeafcafe/loafer/internal/ui"
	"github.com/0xdeafcafe/photon/jsonx"
)

// Clicking a notification runs `loafer open [team [conv ts [thread]]]`
// (internal/notify's app): the loafer that's running goes there and its
// terminal comes forward. With none running, a new one starts in the
// terminal loafer was last in and goes there once it's up.

func terminalPath() string { return filepath.Join(notifyd.Dir(), "terminal") }
func gotoPath() string     { return filepath.Join(notifyd.Dir(), "goto.json") }

func openCmd(args []string) error {
	if len(args) > 4 {
		return errors.New("usage: loafer open [team-id [conversation-id ts [thread-ts]]]")
	}
	a := append(args, "", "", "", "")
	d := draftReq{Open: true, Team: a[0], Conv: a[1], TS: a[2], Thread: a[3]}
	b, _ := os.ReadFile(terminalPath())
	term := strings.TrimSpace(string(b))
	err := hand(d)
	if !errors.Is(err, errNotRunning) {
		if err != nil {
			fmt.Fprintln(os.Stderr, "loafer:", err) // it's still brought forward
		}
		if term == "" {
			return nil
		}
		return exec.Command("/usr/bin/open", "-b", term).Run()
	}
	j, _ := jsonx.Marshal(d)
	_ = os.MkdirAll(notifyd.Dir(), 0o700)
	if err := os.WriteFile(gotoPath(), j, 0o600); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if runsFiles(term) && exec.Command("/usr/bin/open", "-b", term, exe).Run() == nil {
		return nil
	}
	return exec.Command("/usr/bin/open", "-a", "Terminal", exe).Run()
}

// pending is where a notification clicked while loafer was closed asked
// to go, once, if it was just now.
func pending() (ui.Goto, bool) {
	p := gotoPath()
	info, err := os.Stat(p)
	if err != nil {
		return ui.Goto{}, false
	}
	b, _ := os.ReadFile(p)
	_ = os.Remove(p)
	var d draftReq
	if time.Since(info.ModTime()) > time.Minute || jsonx.Unmarshal(b, &d) != nil {
		return ui.Goto{}, false
	}
	return ui.Goto{Team: d.Team, Conv: d.Conv, TS: d.TS, Thread: d.Thread}, true
}

// here keeps the terminal loafer is in, for loafer open to bring forward.
func here() {
	if id := terminalID(); id != "" {
		_ = os.MkdirAll(notifyd.Dir(), 0o700)
		_ = os.WriteFile(terminalPath(), []byte(id), 0o600)
	}
}

// terminalID is the bundle ID of the app loafer's terminal is: macOS hands
// it to everything an app starts, tmux included; TERM_PROGRAM names the
// usual ones when it didn't. (rush's menubar does the same.)
func terminalID() string {
	if id := os.Getenv("__CFBundleIdentifier"); id != "" && !strings.HasPrefix(id, "com.github.0xdeafcafe.loafer") {
		return id
	}
	return map[string]string{
		"Apple_Terminal": "com.apple.Terminal",
		"iTerm.app":      "com.googlecode.iterm2",
		"WarpTerminal":   "dev.warp.Warp-Stable",
		"ghostty":        "com.mitchellh.ghostty",
		"WezTerm":        "com.github.wez.wezterm",
		"vscode":         "com.microsoft.VSCode",
	}[os.Getenv("TERM_PROGRAM")]
}

// runsFiles says whether the terminal runs a program it's asked to open;
// the others don't take one this way, so a new loafer opens in Terminal.
func runsFiles(id string) bool {
	return id == "com.apple.Terminal" || id == "com.googlecode.iterm2" || strings.HasPrefix(id, "dev.warp.Warp")
}

// buildNotifier builds the app clicked notifications open loafer with,
// when this loafer is new to it. Without Xcode's tools they stay
// unclickable.
func buildNotifier() {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err == nil {
		err = notify.Build(exe)
	}
	if err != nil {
		slog.Debug("notifier", "err", err)
	}
}
