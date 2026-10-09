package main

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/ui"
	"github.com/0xdeafcafe/photon/jsonx"
)

// Agents in rush leave drafts in the running loafer: the rush plugin
// (cmd/loafer-rush) has rush run `loafer draft`, which hands the text to
// the TUI over a unix socket only you can reach. Nothing here sends.

// maxDraft is the most a draft may be: about Slack's own limit on a message.
const maxDraft = 40 << 10

type draftReq struct {
	Team   string `json:"team"`
	Conv   string `json:"conv"`
	Thread string `json:"thread,omitempty"`
	Text   string `json:"text"`
}

func draftSock() string {
	d, _ := os.UserCacheDir()
	return filepath.Join(d, "loafer", "draft.sock")
}

// listenDrafts hands drafts for team's conversations to send until stop.
// A second loafer leaves the first the socket.
func listenDrafts(team string, send func(tea.Msg)) (stop func()) {
	path := draftSock()
	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
		slog.Warn("drafts", "err", "another loafer is taking them")
		return func() {}
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.Remove(path) // a crashed run's
	l, err := net.Listen("unix", path)
	if err == nil {
		err = os.Chmod(path, 0o600)
	}
	if err != nil {
		slog.Warn("drafts", "err", err.Error())
		return func() {}
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go serveDraft(c, team, send)
		}
	}()
	return func() { l.Close() }
}

func serveDraft(c net.Conn, team string, send func(tea.Msg)) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	err := takeDraft(c, team, send)
	if err != nil {
		fmt.Fprintln(c, err.Error())
		return
	}
	fmt.Fprintln(c, "ok")
}

func takeDraft(r io.Reader, team string, send func(tea.Msg)) error {
	b, err := io.ReadAll(io.LimitReader(r, maxDraft+4<<10))
	if err != nil {
		return err
	}
	var d draftReq
	switch {
	case jsonx.Unmarshal(b, &d) != nil:
		return errors.New("that isn't a draft")
	case d.Team != team:
		return errors.New("loafer has another workspace open")
	case d.Conv == "":
		return errors.New("no conversation")
	case len(d.Text) > maxDraft:
		return errors.New("the draft is too long for one message")
	}
	done := make(chan error, 1)
	send(ui.Draft{Conv: d.Conv, Thread: d.Thread, Text: d.Text, Done: done})
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		return errors.New("loafer didn't take it in time")
	}
}

// draft is `loafer draft <team-id> <conversation-id> [thread-ts]`, the
// text on stdin. It exits 3 when no loafer is running.
func draft(args []string) int {
	if len(args) < 2 || len(args) > 3 {
		fmt.Fprintln(os.Stderr, "usage: loafer draft <team-id> <conversation-id> [thread-ts] < text")
		return 2
	}
	text, err := io.ReadAll(io.LimitReader(os.Stdin, maxDraft+1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "loafer:", err)
		return 1
	}
	d := draftReq{Team: args[0], Conv: args[1], Text: string(text)}
	if len(args) == 3 {
		d.Thread = args[2]
	}
	c, err := net.DialTimeout("unix", draftSock(), 2*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "loafer isn't running")
		return 3
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	if err := jsonx.MarshalWrite(c, d); err != nil {
		fmt.Fprintln(os.Stderr, "loafer:", err)
		return 1
	}
	_ = c.(*net.UnixConn).CloseWrite()
	out, _ := io.ReadAll(io.LimitReader(c, 4<<10))
	if reply := strings.TrimSpace(string(out)); reply != "ok" {
		fmt.Fprintln(os.Stderr, "loafer:", cmp.Or(reply, "no answer"))
		return 1
	}
	fmt.Println("it's in loafer's composer, for you to send")
	return 0
}
