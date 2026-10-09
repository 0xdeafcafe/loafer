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
	"sync"
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
		fmt.Fprintf(os.Stderr, "usage: loafer [flags] [command]\n\ncommands:\n  login     sign in to a workspace with your Slack session\n  app init  set up loafer's own Slack app (phone pushes, shortcuts)\n  notifyd   notifications while loafer is closed: install, uninstall, status\n  report    zip logs and the latest profile for a bug report\n  draft     put stdin in a running loafer's composer (the rush plugin's)\n  open      go to a message in loafer (a clicked notification's)\n  version   print the version\n\nflags:\n")
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
	case "draft":
		os.Exit(draft(flag.Args()[1:]))
	case "open":
		exitIf(openCmd(flag.Args()[1:]))
		return
	case "notifyd":
		exitIf(notifydCmd(flag.Arg(1)))
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

// run opens every signed-in workspace, the newest shown, signing in
// first when there's none to open or Slack has stopped taking them all.
// One whose sign-in can't be read is left out.
func run() error {
	defer takeOver()()
	for {
		ws, err := slack.Workspaces()
		if err != nil {
			return err
		}
		why := "not signed in yet."
		var all []slack.Creds
		for _, w := range ws {
			creds, err := slack.Load(w.TeamID)
			if err != nil {
				slog.Warn("sign-in", "team", w.TeamID, "err", err)
				why = fmt.Sprintf("couldn't read %s's sign-in (%v).", w.Team, err)
				continue
			}
			all = append(all, creds)
		}
		if len(all) > 0 {
			out, err := open(all)
			if !out {
				return err
			}
			why = fmt.Sprintf("slack signed you out of %s.", all[0].Team)
			if len(all) > 1 {
				why = "slack signed you out of every workspace."
			}
		}
		fmt.Fprintf(os.Stderr, "%s sign in again, or ctrl+c to stop.\n\n", why)
		if err := login(); err != nil {
			return err
		}
	}
}

// open draws each workspace from its cache at once, then boots them
// against Slack behind it, and says whether Slack signed them all out.
func open(all []slack.Creds) (bool, error) {
	ctx, cancel := context.WithCancel(context.Background())
	var saved sync.WaitGroup
	ms := make([]*ui.Model, len(all))
	for i, creds := range all {
		st := store.New()
		path := store.CachePath(creds.TeamID)
		if err := st.Load(path); err != nil {
			slog.Warn("cache load", "err", err) // a bad cache only costs a cold start
		}
		saved.Go(func() { st.WriteBehind(ctx, path) })
		ms[i] = ui.New(ctx, st, slack.New(creds))
		ms[i].WelcomeIfCold(os.Getenv("LOAFER_WELCOME") != "")
	}
	x := ui.NewMulti(ms...)
	p := tea.NewProgram(x)
	teams := make([]string, len(all))
	for i, c := range all {
		teams[i] = c.TeamID
	}
	stopDrafts := listenDrafts(teams, p.Send)
	here()
	go buildNotifier()
	if g, ok := pending(); ok {
		go p.Send(g) // it waits for Run
	}
	_, err := p.Run()
	stopDrafts()
	cancel()
	saved.Wait()
	return err == nil && x.SignedOut(), err
}

func exitIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "loafer:", err)
		os.Exit(1)
	}
}
