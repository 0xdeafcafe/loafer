// Package notifyd is loafer while it's closed: it holds the websocket
// through a store with no message windows, shows what Slack's rules say
// should notify, and hands the websocket to the TUI while one is open
// (handover.go). launchd keeps it running (launchd.go).
package notifyd

import (
	"bufio"
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/0xdeafcafe/loafer/internal/notify"
	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
)

// Daemon is notifyd.
type Daemon struct {
	Dir    string                                   // the socket and the locks (Dir())
	Client func() (*slack.Client, error)            // the sign-in, read again each time it goes live
	Show   func(context.Context, notify.Note) error // notify.Show: there's no terminal to write to
}

// ErrRunning is Run's when another notifyd has the run directory.
var ErrRunning = errors.New("notifyd is already running")

// SignedOutNote is shown once when Slack stops taking the sign-in.
var SignedOutNote = notify.Note{Title: "loafer", Body: "loafer was signed out; run loafer"}

type ask struct {
	what string
	conn net.Conn
}

// Run holds the websocket while no TUI is open, until ctx ends. Signed
// out, it says so once and waits for a TUI to come and go (where you
// sign in again) rather than trying again by itself.
func (d *Daemon) Run(ctx context.Context) error {
	if err := os.MkdirAll(d.Dir, 0o700); err != nil {
		return err
	}
	one, err := lock(filepath.Join(d.Dir, "notifyd.lock"), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrRunning
	}
	if err != nil {
		return err
	}
	defer one.Close()
	_ = os.Remove(sockPath(d.Dir)) // left by a notifyd that didn't get to close it
	ln, err := net.Listen("unix", sockPath(d.Dir))
	if err != nil {
		return err
	}
	defer ln.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	asks := make(chan ask)
	go accept(ctx, ln, asks)

	var (
		st      *store.Store // set while live
		stop    context.CancelFunc
		ended   = make(chan error, 1)
		out     bool // signed out, and waiting for a TUI
		free    = make(chan struct{})
		waiters int // waitFrees whose word hasn't been taken
		th      notify.Throttle
		flush   <-chan time.Time // armed only while the throttle holds a note
	)
	wait := func() {
		waiters++
		go waitFree(ctx, tuiLock(d.Dir), free)
	}
	park := func() {
		if stop != nil {
			stop()
			<-ended
			st, stop = nil, nil
		}
	}
	show := func(n notify.Note) {
		go func() {
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err := d.Show(ctx, n); err != nil {
				slog.Warn("notifyd.show", "err", err)
			}
		}()
	}
	signedOut := func(err error) {
		slog.Warn("notifyd", "signed_out", err.Error())
		out = true
		show(SignedOutNote)
	}
	wait() // a TUI may be open already

	for {
		var notes <-chan notify.Note
		if st != nil {
			notes = st.Notes()
		}
		select {
		case <-ctx.Done():
			park()
			return nil

		case <-free:
			waiters--
			if held(tuiLock(d.Dir)) { // a TUI took it since
				if waiters == 0 {
					wait()
				}
				continue
			}
			if stop != nil {
				continue
			}
			c, err := d.Client()
			if err != nil {
				signedOut(err)
				continue
			}
			// A new store each time: the TUI may have signed in again, or
			// changed the prefs, while it had the websocket.
			st = store.New()
			lctx, cancel := context.WithCancel(ctx)
			stop = cancel
			go func(st *store.Store) { ended <- st.Live(lctx, c) }(st)
			slog.Info("notifyd", "state", "live")

		case err := <-ended:
			stop()
			st, stop = nil, nil
			if err != nil {
				signedOut(err)
			}

		case a := <-asks:
			reply := "parked"
			switch a.what {
			case "take":
				park()
				out = false
				if waiters == 0 {
					wait()
				}
				slog.Info("notifyd", "state", "parked")
			case "status":
				switch {
				case st != nil:
					reply = "connecting"
					st.Read(func(v store.View) {
						if l := v.Link(); l != "" {
							reply = l
						}
					})
				case out:
					reply = "signed out"
				}
			default:
				reply = "?"
			}
			_, _ = a.conn.Write([]byte(reply + "\n"))
			a.conn.Close()

		case n := <-notes:
			now := time.Now()
			if n, ok := th.Push(n, now); ok {
				show(n)
			} else if flush == nil {
				flush = time.After(th.Wait(now))
			}

		case <-flush:
			flush = nil
			if n, ok := th.Flush(time.Now()); ok {
				show(n)
			}
		}
	}
}

// accept reads one line from each who connects and hands it to Run.
func accept(ctx context.Context, ln net.Listener, asks chan<- ask) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			_ = c.SetDeadline(time.Now().Add(5 * time.Second))
			line, err := bufio.NewReader(c).ReadString('\n')
			if err != nil {
				c.Close()
				return
			}
			select {
			case asks <- ask{strings.TrimSpace(line), c}:
			case <-ctx.Done():
				c.Close()
			}
		}()
	}
}

// waitFree blocks until no TUI holds path, then says so. It lets go at
// once: Run looks again before going live, since a TUI may come between.
func waitFree(ctx context.Context, path string, free chan<- struct{}) {
	f, err := lock(path, syscall.LOCK_EX)
	if err != nil {
		slog.Warn("notifyd.lock", "err", err) // without the lock, go live rather than never
	} else {
		f.Close()
	}
	select {
	case free <- struct{}{}:
	case <-ctx.Done():
	}
}
