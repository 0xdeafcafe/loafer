package rushlink

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fake puts a rush on the PATH that logs its arguments and stdin to
// calls, and answers watch with canned lines (or as RUSH_WATCH says).
const fake = `#!/bin/sh
dir=$(dirname "$0")
echo "$*" >> "$dir/calls"
case "$2" in
start|send) cat >> "$dir/calls"; echo >> "$dir/calls"; touch "$dir/woken" ;;
watch)
  case "$RUSH_WATCH" in
  fail) echo 'rush: unknown session command "watch"' >&2; exit 2 ;;
  gone) echo '{"error":"session 0a1b2c3d is not running"}'; exit 1 ;;
  asleep) [ -f "$dir/woken" ] || { echo '{"error":"session 0a1b2c3d is not running"}'; exit 1; } ;;
  hang) exec sleep 30 ;;
  esac
  echo '{"costUsd":0.01,"detail":"thinking","event":"info","needs":"","session":"0a1b2c3d","state":"working"}'
  echo 'not json'
  echo '{"event":"delta","session":"0a1b2c3d","text":"Three"}'
  echo '{"doing":"reading notes.md","event":"tool","name":"Read","session":"0a1b2c3d"}'
  echo '{"event":"text","session":"0a1b2c3d","text":"Three things happened."}'
  echo '{"costUsd":0.04,"event":"result","isError":false,"session":"0a1b2c3d","text":"Three things happened.","turns":1}'
  ;;
esac
`

func setup(t *testing.T) (*CLI, string) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rush"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin") // the fake first; the shell's tools after
	t.Setenv("HOME", dir)
	c, err := Find()
	if err != nil {
		t.Fatal(err)
	}
	return c, filepath.Join(dir, "calls")
}

func drain(t *testing.T, ch <-chan Event) []Event {
	var out []Event
	timeout := time.After(5 * time.Second)
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, e)
		case <-timeout:
			t.Fatal("the watch never ended")
		}
	}
}

func TestFindWithoutRush(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Find(); err == nil {
		t.Fatal("found a rush that isn't there")
	}
}

func TestStartAndWatch(t *testing.T) {
	c, calls := setup(t)
	s := Session{ID: "0a1b2c3d-0000-4000-8000-000000000000", Name: "loafer: summarise #dev", Meta: map[string]string{"team": "T1", "app": "loafer"}}
	ch, err := Ask(context.Background(), c, s, true, "what happened?\nin #dev")
	if err != nil {
		t.Fatal(err)
	}
	evs := drain(t, ch)
	var kinds []string
	for _, e := range evs {
		kinds = append(kinds, e.Type)
	}
	if want := []string{"info", "delta", "tool", "text", "result"}; !slices.Equal(kinds, want) {
		t.Fatalf("events %v, want %v", kinds, want)
	}
	if r := evs[4]; r.Text != "Three things happened." || r.CostUSD != 0.04 || r.Turns != 1 {
		t.Fatalf("result %+v", r)
	}
	if evs[2].Doing != "reading notes.md" {
		t.Fatalf("tool %+v", evs[2])
	}
	b, _ := os.ReadFile(calls)
	log := string(b)
	for _, want := range []string{
		"session start --cwd " + c.Scratch + " --agent claude --permission-mode plan --session-id " + s.ID +
			" --name loafer: summarise #dev --prompt-file - --json --meta app=loafer --meta team=T1\nwhat happened?\nin #dev\n",
		"session watch 0a1b2c3d --json --until-idle",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("calls\n%s\nwant\n%s", log, want)
		}
	}
	if st, err := os.Stat(c.Scratch); err != nil || !st.IsDir() {
		t.Fatal("no scratch folder")
	}
}

func TestFollowUpWatchesFirst(t *testing.T) {
	c, calls := setup(t)
	ch, err := Ask(context.Background(), c, Session{ID: "0a1b2c3d-0000-4000-8000-000000000000"}, false, "and the db?")
	if err != nil {
		t.Fatal(err)
	}
	if evs := drain(t, ch); len(evs) != 5 || evs[0].Type != "info" {
		t.Fatalf("events %+v", evs)
	}
	// The watch's own line may land between send's two.
	if b, _ := os.ReadFile(calls); !strings.Contains(string(b), "session send 0a1b2c3d\n") || !strings.Contains(string(b), "and the db?\n") {
		t.Fatalf("calls\n%s", b)
	}
	// A stopped session: the first watch can't, send wakes it, the second can.
	t.Setenv("RUSH_WATCH", "asleep")
	ch, err = Ask(context.Background(), c, Session{ID: "0a1b2c3d-0000-4000-8000-000000000000"}, false, "still there?")
	if err != nil {
		t.Fatal(err)
	}
	if evs := drain(t, ch); len(evs) != 5 || evs[4].Type != "result" {
		t.Fatalf("after waking %+v", evs)
	}
	// The order, without two processes racing to the log.
	var l order
	if _, err := Ask(context.Background(), &l, Session{ID: "x"}, false, "hi"); err != nil || strings.Join(l, " ") != "watch send" {
		t.Fatalf("%v %v", l, err)
	}
}

type order []string

func (o *order) Start(context.Context, Session, string) error { *o = append(*o, "start"); return nil }
func (o *order) Send(context.Context, string, string) error   { *o = append(*o, "send"); return nil }
func (o *order) Open(string) *exec.Cmd                        { return nil }
func (o *order) Watch(context.Context, string) (<-chan Event, error) {
	*o = append(*o, "watch")
	ch := make(chan Event)
	close(ch)
	return ch, nil
}

func TestWatchSaysWhy(t *testing.T) {
	c, _ := setup(t)
	t.Setenv("RUSH_WATCH", "gone")
	ch, err := c.Watch(context.Background(), "0a1b2c3d")
	if err != nil {
		t.Fatal(err)
	}
	evs := drain(t, ch)
	if len(evs) != 1 || evs[0].Type != "failed" || evs[0].Text != "session 0a1b2c3d is not running" {
		t.Fatalf("events %+v", evs)
	}
}

func TestWatchFails(t *testing.T) {
	c, _ := setup(t)
	t.Setenv("RUSH_WATCH", "fail")
	ch, err := c.Watch(context.Background(), "0a1b2c3d")
	if err != nil {
		t.Fatal(err)
	}
	evs := drain(t, ch)
	if len(evs) != 1 || evs[0].Type != "failed" || evs[0].Text != `unknown session command "watch"` {
		t.Fatalf("events %+v", evs)
	}
}

func TestWatchCancels(t *testing.T) {
	c, _ := setup(t)
	t.Setenv("RUSH_WATCH", "hang")
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := c.Watch(ctx, "0a1b2c3d")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if evs := drain(t, ch); len(evs) != 0 {
		t.Fatalf("events after cancelling %+v", evs)
	}
}

func TestParse(t *testing.T) {
	for line, want := range map[string]string{
		`{"event":"closed"}`:          "closed",
		`{"type":"sent","text":"hi"}`: "sent",
		`{"text":"no kind"}`:          "",
		`{"error":"not found"}`:       "failed",
		`nope`:                        "",
	} {
		e, ok := Parse([]byte(line))
		if e.Type != want || ok != (want != "") {
			t.Errorf("%s: %q %v", line, e.Type, ok)
		}
	}
}
