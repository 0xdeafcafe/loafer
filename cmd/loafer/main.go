// Command loafer is a terminal Slack client.
package main

import (
	"context"
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
	demoMode := flag.Bool("demo", false, "open a made-up workspace, with no sign-in, to try loafer")
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
	if *demoMode {
		exitIf(demo())
		return
	}
	exitIf(run())
}

// run opens the default workspace, signing in first when there's no
// sign-in to open or Slack has stopped taking it.
func run() error {
	for {
		ws, err := slack.Workspaces()
		if err != nil {
			return err
		}
		why := "not signed in yet."
		if len(ws) > 0 {
			creds, err := slack.Load(ws[0].TeamID)
			if err == nil {
				out, err := open(creds)
				if !out {
					return err
				}
				why = fmt.Sprintf("slack signed you out of %s.", creds.Team)
			} else {
				why = fmt.Sprintf("couldn't read %s's sign-in (%v).", ws[0].Team, err)
			}
		}
		fmt.Fprintf(os.Stderr, "%s sign in again, or ctrl+c to stop.\n\n", why)
		if err := login(); err != nil {
			return err
		}
	}
}

// open draws creds' workspace from the cache at once, then boots it
// against Slack behind it, and says whether Slack signed it out.
func open(creds slack.Creds) (bool, error) {
	ctx, cancel := context.WithCancel(context.Background())
	st := store.New()
	path := store.CachePath(creds.TeamID)
	if err := st.Load(path); err != nil {
		slog.Warn("cache load", "err", err) // a bad cache only costs a cold start
	}
	saved := make(chan struct{})
	go func() { st.WriteBehind(ctx, path); close(saved) }()
	m := ui.New(ctx, st, slack.New(creds))
	_, err := tea.NewProgram(m).Run()
	cancel()
	<-saved
	return err == nil && m.SignedOut(), err
}

func exitIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "loafer:", err)
		os.Exit(1)
	}
}
