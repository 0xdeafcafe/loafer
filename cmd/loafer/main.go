// Command loafer is a terminal Slack client.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/obs"
	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/loafer/internal/ui"
)

var version = "dev"

func main() {
	trace := flag.String("trace", "", "log full payloads for these kinds: api,ws,render,store or all")
	pprofAddr := flag.String("pprof", "", "serve net/http/pprof on this address, e.g. localhost:6061")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: loafer [flags] [command]\n\ncommands:\n  login     sign in to a workspace with your Slack session\n  app init  set up loafer's own Slack app (phone pushes, shortcuts)\n  report    zip logs and the latest profile for a bug report\n  version   print the version\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if os.Getenv("LOAFER_MEMPROFILE") == "" {
		runtime.MemProfileRate = 0
	}
	if os.Getenv("GOGC") == "" {
		// Collect at half again the live heap: a few MB less resident for
		// about one more collection a second (as rush's view does).
		debug.SetGCPercent(50)
	}
	if *trace != "" {
		obs.Trace(*trace)
	}

	switch flag.Arg(0) {
	case "version":
		fmt.Println("loafer", version)
		return
	case "login":
		stop := obs.Start(filepath.Join(obs.Dir(), "loafer.jsonl"))
		err := login()
		stop()
		exitIf(err)
		return
	case "app":
		if flag.Arg(1) != "init" {
			flag.Usage()
			os.Exit(2)
		}
		stop := obs.Start(filepath.Join(obs.Dir(), "loafer.jsonl"))
		err := appInit()
		stop()
		exitIf(err)
		return
	case "report":
		name := fmt.Sprintf("loafer-report-%s.zip", time.Now().Format("2006-01-02T15-04-05"))
		f, err := os.Create(name)
		exitIf(err)
		exitIf(obs.Report(f, version))
		exitIf(f.Close())
		abs, _ := filepath.Abs(name)
		fmt.Println(abs)
		return
	case "":
	default:
		flag.Usage()
		os.Exit(2)
	}

	stop := obs.Start(filepath.Join(obs.Dir(), "loafer.jsonl"))
	defer stop()
	if *pprofAddr != "" {
		obs.Serve(*pprofAddr)
	}
	exitIf(run())
}

// run opens the default workspace: drawn from the cache at once, then
// booted against Slack behind it.
func run() error {
	ws, err := slack.Workspaces()
	if err != nil {
		return err
	}
	if len(ws) == 0 {
		return errors.New("not signed in yet: run loafer login")
	}
	creds, err := slack.Load(ws[0].TeamID)
	if err != nil {
		return fmt.Errorf("reading %s's sign-in: %w", ws[0].Team, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	st := store.New()
	path := store.CachePath(creds.TeamID)
	if err := st.Load(path); err != nil {
		slog.Warn("cache load", "err", err) // a bad cache only costs a cold start
	}
	saved := make(chan struct{})
	go func() { st.WriteBehind(ctx, path); close(saved) }()
	_, err = tea.NewProgram(ui.New(ctx, st, slack.New(creds))).Run()
	cancel()
	<-saved
	return err
}

func exitIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "loafer:", err)
		os.Exit(1)
	}
}
