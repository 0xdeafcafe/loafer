// Package rushlink is how loafer asks your own agent, through rush
// (docs/claude-pane.md). It runs only when the claude pane is used, and
// never on the ui goroutine.
//
// Today it's rush's session cli. A rush plugin forwarding
// sessions.subscribe to a loafer socket, as rush's kanban example does
// for kanban-code, would be another Link.
package rushlink

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/0xdeafcafe/photon/jsonx"
)

// Event is something a session did, in the words rush gives plugins
// (sessions.subscribe): info, sent, delta, text, tool, result, closed.
// rushlink adds failed, for a watch that ended without a result.
type Event struct {
	Type    string  `json:"type"`
	State   string  `json:"state"` // info: starting, working, blocked, idle, stopped
	Detail  string  `json:"detail"`
	Needs   string  `json:"needs"` // info: what it's blocked on
	Text    string  `json:"text"`  // sent, delta, text, result, failed
	Name    string  `json:"name"`  // tool
	Doing   string  `json:"doing"` // tool, in words
	IsError bool    `json:"isError"`
	CostUSD float64 `json:"costUsd"`
	Turns   int     `json:"turns"`
}

// Parse reads one line of `rush session watch --json`. The kind is
// "event" there and "type" in a plugin's session.event; either will do.
// A watch that fails says {"error":"…"} last, which reads as failed.
func Parse(line []byte) (Event, bool) {
	var e Event
	var more struct {
		Event string `json:"event"`
		Error string `json:"error"`
	}
	if jsonx.Unmarshal(line, &e) != nil || jsonx.Unmarshal(line, &more) != nil {
		return Event{}, false
	}
	switch {
	case more.Event != "":
		e.Type = more.Event
	case more.Error != "":
		e.Type, e.Text = "failed", more.Error
	}
	return e, e.Type != ""
}

// Session is what a new session is started with. Meta tags it, as
// kanban tags its sessions with their card, so `rush session list --meta
// app=loafer` finds loafer's and each says what it was about.
type Session struct {
	ID   string // a uuid, made by loafer so it's known before rush answers
	Name string // "loafer: summarise #prod-alerts"
	Meta map[string]string
}

// Link is a way to rush.
type Link interface {
	// Start begins a session with its first message.
	Start(ctx context.Context, s Session, prompt string) error
	// Send is a follow-up, once the turn before has ended.
	Send(ctx context.Context, id, text string) error
	// Watch streams what the session does until it's idle again, then
	// closes the channel. Cancelling ctx stops it.
	Watch(ctx context.Context, id string) (<-chan Event, error)
	// Open is rush's own view of the session, to run in the terminal.
	Open(id string) *exec.Cmd
}

// NewID is a random uuid for a session.
func NewID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Short is the id rush knows a session by: the uuid's first 8 digits.
func Short(id string) string { return id[:min(8, len(id))] }

// CLI is rush's session commands, run as a program with arguments,
// never through a shell.
type CLI struct {
	Path    string // the rush binary
	Scratch string // the empty folder sessions run in
}

// Find is rush on the PATH, or an error saying how to get it.
func Find() (*CLI, error) {
	p, err := exec.LookPath("rush")
	if err != nil {
		return nil, errors.New("rush isn't on the PATH")
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &CLI{Path: p, Scratch: filepath.Join(dir, "loafer", "claude")}, nil
}

// Start runs `rush session start`, the prompt on stdin. Plan mode in an
// empty folder: the prompt is other people's words, so a tool it talks
// the agent into still has rush ask first, and there's nothing to read.
func (c *CLI) Start(ctx context.Context, s Session, prompt string) error {
	if err := os.MkdirAll(c.Scratch, 0o700); err != nil {
		return err
	}
	args := []string{"session", "start", "--cwd", c.Scratch, "--agent", "claude", "--permission-mode", "plan",
		"--session-id", s.ID, "--name", s.Name, "--prompt-file", "-", "--json"}
	for _, k := range slices.Sorted(maps.Keys(s.Meta)) {
		args = append(args, "--meta", k+"="+s.Meta[k])
	}
	return c.run(ctx, prompt, args...)
}

// Send runs `rush session send`, the text on stdin. rush brings a
// stopped session back itself.
func (c *CLI) Send(ctx context.Context, id, text string) error {
	return c.run(ctx, text, "session", "send", Short(id))
}

func (c *CLI) run(ctx context.Context, stdin string, args ...string) error {
	cmd := exec.CommandContext(ctx, c.Path, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return failure(err, errb.String())
	}
	return nil
}

// Watch runs `rush session watch <id> --json --until-idle`.
func (c *CLI) Watch(ctx context.Context, id string) (<-chan Event, error) {
	cmd := exec.CommandContext(ctx, c.Path, "session", "watch", Short(id), "--json", "--until-idle")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	ch := make(chan Event, 16)
	go func() {
		defer close(ch)
		ended := false
		sc := bufio.NewScanner(out)
		sc.Buffer(nil, 8<<20) // a result carries the whole answer
		for sc.Scan() {
			e, ok := Parse(sc.Bytes())
			if !ok {
				continue
			}
			ended = ended || e.Type == "result" || e.Type == "closed" || e.Type == "failed"
			send(ctx, ch, e)
		}
		if err := cmd.Wait(); err != nil && !ended && ctx.Err() == nil {
			send(ctx, ch, Event{Type: "failed", Text: failure(err, errb.String()).Error()})
		}
	}()
	return ch, nil
}

// Open is `rush open <id> --hosted`: rush's view of the session, full
// screen, until ctrl+q.
func (c *CLI) Open(id string) *exec.Cmd {
	return exec.Command(c.Path, "open", Short(id), "--hosted")
}

// failure is err with the last thing rush said about it.
func failure(err error, stderr string) error {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		return errors.New(strings.TrimPrefix(last, "rush: "))
	}
	return err
}

// Ask starts or continues session s with text and follows the turn.
// watch replays the turn under way when it connects, so a new session is
// watched once it's started; an idle one waits for the next turn, so a
// follow-up is watched before it's sent.
func Ask(ctx context.Context, l Link, s Session, first bool, text string) (<-chan Event, error) {
	if first {
		if err := l.Start(ctx, s, text); err != nil {
			return nil, err
		}
		return l.Watch(ctx, s.ID)
	}
	ctx, stop := context.WithCancel(ctx)
	ch, err := l.Watch(ctx, s.ID)
	if err == nil {
		err = l.Send(ctx, s.ID, text)
	}
	if err != nil {
		stop() // and the watch with it
		return nil, err
	}
	out := make(chan Event, 16)
	go func() {
		defer stop()
		defer close(out)
		again := true
		for ch != nil {
			cur := ch
			ch = nil
			for e := range cur {
				// watch doesn't wake a stopped session, send does: now it's
				// been sent, a second watch replays the turn.
				if again && e.Type == "failed" && strings.HasSuffix(e.Text, "is not running") {
					again = false
					if ch, err = l.Watch(ctx, s.ID); err == nil {
						continue
					}
				}
				send(ctx, out, e)
			}
		}
	}()
	return out, nil
}

// send passes e on, unless nobody's listening any more.
func send(ctx context.Context, ch chan<- Event, e Event) {
	select {
	case ch <- e:
	case <-ctx.Done():
	}
}
