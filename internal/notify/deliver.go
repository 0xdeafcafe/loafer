package notify

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/0xdeafcafe/loafer/internal/mrkdwn"
)

// Note is a notification: the conversation's name or the person, and the
// message as plain text.
type Note struct {
	Conv  string
	Title string
	Body  string
}

// Plain is a message's text without its mrkdwn: links by their labels,
// people and channels by name, one line, cut to a notification's size.
// user names a user id.
func Plain(text string, user func(id string) string) string {
	var b strings.Builder
	for _, l := range mrkdwn.Parse(text) {
		for _, s := range l.Spans {
			switch s.Kind {
			case mrkdwn.User:
				b.WriteString("@" + user(s.Target))
			case mrkdwn.Channel:
				b.WriteString("#" + strings.TrimPrefix(s.Text, "#"))
			case mrkdwn.Emoji:
				b.WriteString(":" + s.Text + ":")
			default:
				b.WriteString(s.Text)
			}
		}
		b.WriteByte(' ')
	}
	return cut(clean(b.String()), 200)
}

// clean makes s fit in a notification: no control characters, which
// would otherwise reach the terminal as escapes, and no runs of space.
func clean(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }), " ")
}

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// Escape is n as the terminal's own notification escape, or "" when this
// terminal has none that we know (or it's under tmux, which would drop
// it). env is os.Getenv.
func Escape(env func(string) string, n Note) string {
	if env("TMUX") != "" {
		return ""
	}
	title, body := cut(clean(n.Title), 80), cut(clean(n.Body), 200)
	switch {
	case env("TERM_PROGRAM") == "iTerm.app", env("TERM_PROGRAM") == "WezTerm":
		return "\x1b]9;" + title + ": " + body + "\x07"
	case env("TERM_PROGRAM") == "ghostty":
		return "\x1b]777;notify;" + strings.ReplaceAll(title, ";", ",") + ";" + body + "\x07"
	case env("KITTY_WINDOW_ID") != "":
		// Kitty's protocol: a title chunk, then the body to finish it.
		return "\x1b]99;i=1:d=0;" + title + "\x1b\\\x1b]99;i=1:d=1:p=body;" + body + "\x1b\\"
	}
	return ""
}

// Osascript shows n through macOS. The text goes in as arguments to a
// fixed script, never into the script, so nothing in a message can be
// taken for AppleScript.
func Osascript(ctx context.Context, n Note) error {
	if runtime.GOOS != "darwin" {
		return errors.New("no notifier on " + runtime.GOOS)
	}
	return exec.CommandContext(ctx, "osascript",
		"-e", "on run argv",
		"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
		"-e", "end run",
		"--", cut(clean(n.Title), 80), cut(clean(n.Body), 200)).Run()
}

// Gap is how long a burst of notifications has to wait for the next to
// show; those that arrive meanwhile are shown as one.
const Gap = 3 * time.Second

// Throttle lets one note through now and then and holds the rest of a
// burst. The zero value is ready. Not safe for concurrent use.
type Throttle struct {
	last time.Time
	held []Note
}

// Push takes n, and gives it back to show now if it's been a Gap since
// the last was shown. If not it's held, for Flush.
func (t *Throttle) Push(n Note, now time.Time) (Note, bool) {
	if len(t.held) == 0 && now.Sub(t.last) >= Gap {
		t.last = now
		return n, true
	}
	t.held = append(t.held, n)
	return Note{}, false
}

// Wait is how long until Flush has something to show, or 0 if nothing is held.
func (t *Throttle) Wait(now time.Time) time.Duration {
	if len(t.held) == 0 {
		return 0
	}
	return max(time.Millisecond, Gap-now.Sub(t.last))
}

// Flush gives what was held as one note: the one that was, or a count
// and where from.
func (t *Throttle) Flush(now time.Time) (Note, bool) {
	held := t.held
	t.held = nil
	switch len(held) {
	case 0:
		return Note{}, false
	case 1:
		t.last = now
		return held[0], true
	}
	var from []string
	for _, n := range held {
		if !slices.Contains(from, n.Title) {
			from = append(from, n.Title)
		}
	}
	t.last = now
	return Note{Title: fmt.Sprintf("%d new messages", len(held)), Body: strings.Join(from, ", ")}, true
}
