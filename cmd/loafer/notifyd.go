package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	"github.com/0xdeafcafe/loafer/internal/notify"
	"github.com/0xdeafcafe/loafer/internal/notifyd"
	"github.com/0xdeafcafe/loafer/internal/obs"
	"github.com/0xdeafcafe/loafer/internal/slack"
)

// notifydCmd is `loafer notifyd [install|uninstall|status]`: with nothing
// after it, notifyd itself, which launchd runs.
func notifydCmd(sub string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	a := notifyd.Agent{Dir: filepath.Join(home, "Library", "LaunchAgents"), Run: notifyd.Launchctl}
	switch sub {
	case "":
		stop := obs.Start(filepath.Join(obs.Dir(), "notifyd.jsonl"))
		defer stop()
		debug.SetMemoryLimit(12 << 20) // its budget is 15 MB resident
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		d := &notifyd.Daemon{Dir: notifyd.Dir(), Client: signIn, Show: notify.Show}
		err := d.Run(ctx)
		if errors.Is(err, notifyd.ErrRunning) {
			fmt.Fprintln(os.Stderr, "loafer:", err)
			return nil // a clean exit, so launchd doesn't start it again
		}
		return err
	case "install":
		exe, err := os.Executable()
		if err == nil {
			exe, err = filepath.EvalSymlinks(exe)
		}
		if err != nil {
			return err
		}
		if err := a.Install(exe); err != nil {
			return err
		}
		fmt.Println("installed", a.Path())
		if err := notify.Build(exe); err != nil {
			fmt.Println("notifications won't open loafer when clicked:", err)
		}
	case "uninstall":
		if err := a.Uninstall(); err != nil {
			return err
		}
		fmt.Println("uninstalled")
	case "status":
		switch {
		case a.Loaded():
			fmt.Println("launchd:   loaded,", a.Path())
		case a.Installed():
			fmt.Println("launchd:   installed, not loaded,", a.Path())
		default:
			fmt.Println("launchd:   not installed")
		}
		dir := notifyd.Dir()
		s, err := notifyd.Ask(dir, "status")
		switch {
		case errors.Is(err, notifyd.ErrNotRunning) && notifyd.TUI(dir):
			fmt.Println("websocket: the tui has it; notifyd isn't running")
		case errors.Is(err, notifyd.ErrNotRunning):
			fmt.Println("websocket: nobody has it; notifyd isn't running")
		case err != nil:
			return err
		case s == "parked":
			fmt.Println("websocket: the tui has it; notifyd is parked")
		case s == "signed out":
			fmt.Println("websocket: nobody has it; notifyd was signed out, run loafer")
		default:
			fmt.Println("websocket: notifyd has it,", s)
		}
	default:
		return errors.New("usage: loafer notifyd [install|uninstall|status]")
	}
	return nil
}

// signIn is the default workspace's client, as run opens it.
func signIn() (*slack.Client, error) {
	ws, err := slack.Workspaces()
	if err != nil {
		return nil, err
	}
	if len(ws) == 0 {
		return nil, errors.New("not signed in")
	}
	creds, err := slack.Load(ws[0].TeamID)
	if err != nil {
		return nil, err
	}
	return slack.New(creds), nil
}

// takeOver has notifyd, if it's running, hand over the websocket until
// loafer closes.
func takeOver() (release func()) {
	release, err := notifyd.Take(notifyd.Dir())
	if err != nil {
		slog.Warn("notifyd.take", "err", err) // run anyway, as without notifyd
	}
	return release
}
