package notifyd

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/loafer/internal/notify"
	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/slacktest"
	"github.com/0xdeafcafe/loafer/internal/store"
)

// runDir is a run directory under /tmp: a unix socket's path is cut at
// 104 bytes, which $TMPDIR can be past already.
func runDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "nd")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// daemon starts notifyd against srv in a run directory of its own, and
// gives what it shows.
func daemon(t *testing.T, srv *slacktest.Server) (string, <-chan notify.Note) {
	t.Helper()
	dir := runDir(t)
	shown := make(chan notify.Note, 16)
	d := &Daemon{
		Dir:    dir,
		Client: func() (*slack.Client, error) { return srv.Client(), nil },
		Show:   func(_ context.Context, n notify.Note) error { shown <- n; return nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
		os.RemoveAll(dir)
	})
	return dir, shown
}

func until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func status(dir string) string {
	s, _ := Ask(dir, "status")
	return s
}

func note(t *testing.T, notes <-chan notify.Note, body string) {
	t.Helper()
	select {
	case n := <-notes:
		if !strings.Contains(n.Body, body) {
			t.Fatalf("note %+v, want %q", n, body)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no note for %q", body)
	}
}

func none(t *testing.T, notes <-chan notify.Note) {
	t.Helper()
	select {
	case n := <-notes:
		t.Fatalf("unexpected note %+v", n)
	case <-time.After(200 * time.Millisecond):
	}
}

// Two in-process instances, notifyd and a TUI's store, against one fake
// Slack: the TUI takes the websocket, notifyd parks, and it comes back
// when the TUI goes. There's never more than one socket open.
func TestHandover(t *testing.T) {
	srv := slacktest.New()
	defer srv.Close()
	dir, shown := daemon(t, srv)

	until(t, "notifyd live", func() bool { return status(dir) == "live" })
	srv.Post(slacktest.PriyaDM, slacktest.Priya, "are you about?", "")
	note(t, shown, "are you about?")

	release, err := Take(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s := status(dir); s != "parked" || !TUI(dir) {
		t.Fatalf("after take: %q, tui %v", s, TUI(dir))
	}
	until(t, "notifyd's socket closed", func() bool { return srv.Sockets() == 0 })

	// The TUI's own connection, through the same store.Live.
	tst := store.New()
	tctx, tcancel := context.WithCancel(context.Background())
	tdone := make(chan error, 1)
	go func() { tdone <- tst.Live(tctx, srv.Client()) }()
	link := func() (l string) { tst.Read(func(v store.View) { l = v.Link() }); return l }
	until(t, "the tui live", func() bool { return link() == "live" })

	// A second TUI while notifyd is parked takes at once.
	release2, err := Take(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv.Post(slacktest.JoDM, slacktest.Jo, "lunch?", "")
	select {
	case n := <-tst.Notes():
		if !strings.Contains(n.Body, "lunch?") {
			t.Fatalf("tui note %+v", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the tui wasn't notified")
	}
	none(t, shown)
	if n := srv.Sockets(); n != 1 {
		t.Fatalf("%d sockets open while the tui has it", n)
	}

	tcancel()
	<-tdone
	release()
	time.Sleep(50 * time.Millisecond)
	if s := status(dir); s != "parked" {
		t.Fatalf("with one tui left: %q", s)
	}
	release2()
	until(t, "notifyd back", func() bool { return status(dir) == "live" && srv.Sockets() == 1 })
	srv.Post(slacktest.PriyaDM, slacktest.Priya, "back again", "")
	note(t, shown, "back again")
}

// Signed out, notifyd says so once and tries again only after a TUI has
// been and gone.
func TestSignedOut(t *testing.T) {
	srv := slacktest.New()
	defer srv.Close()
	srv.SetSignedOut(true)
	dir, shown := daemon(t, srv)

	note(t, shown, SignedOutNote.Body)
	until(t, "signed out", func() bool { return status(dir) == "signed out" })
	calls := len(srv.Calls())
	none(t, shown)
	if n := len(srv.Calls()); n != calls {
		t.Fatalf("%d calls while signed out", n-calls)
	}

	release, err := Take(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetSignedOut(false) // signed in again in the TUI
	release()
	until(t, "notifyd back", func() bool { return status(dir) == "live" })
}

func TestOneNotifyd(t *testing.T) {
	srv := slacktest.New()
	defer srv.Close()
	dir, _ := daemon(t, srv)
	until(t, "notifyd up", func() bool { return status(dir) != "" })
	d := &Daemon{Dir: dir}
	if err := d.Run(context.Background()); !errors.Is(err, ErrRunning) {
		t.Fatalf("a second notifyd: %v", err)
	}
}

// Without notifyd, a TUI takes the lock and gets on with it.
func TestTakeAlone(t *testing.T) {
	dir := runDir(t)
	defer os.RemoveAll(dir)
	release, err := Take(dir)
	if err != nil || !TUI(dir) {
		t.Fatalf("take: %v, tui %v", err, TUI(dir))
	}
	release()
	if TUI(dir) {
		t.Fatal("still held after release")
	}
	if _, err := Ask(dir, "status"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("ask: %v", err)
	}
}
