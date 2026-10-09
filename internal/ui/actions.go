package ui

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/termimg"
)

// The message actions menu (docs/ui.md, Message actions menu). `.` on a
// message lists what can be done to it, each with its key, and the keys
// work without the menu: do is the one place they all land. What changes
// changes here at once and at Slack in the background, put back if Slack
// refuses (act).

type actions struct {
	menu   menu
	unread string // the conversation marked unread by hand: watching it isn't reading it
}

type menu struct {
	on     bool
	remind bool // the chooser of when, not the list of actions
	thread bool // opened over the thread pane
	link   bool // the message has a link to open
	conv   string
	msg    slack.Message
	at     int
}

type menuItem struct {
	key, glyph, label, tail string
	danger                  bool
}

// actedMsg is an action's call coming back. again is a save that had to
// wait for the Later list, to know whether the message is on it.
type actedMsg struct {
	what  string
	err   error
	again *slack.Message
	conv  string
}

// remindChoices are the chooser's, as Slack's: in 20 minutes, 1 hour,
// 3 hours, tomorrow at 9 and next week (the Monday, at 9).
var remindChoices = [...]struct {
	label string
	at    func(now time.Time) time.Time
}{
	{"in 20 minutes", func(n time.Time) time.Time { return n.Add(20 * time.Minute) }},
	{"in 1 hour", func(n time.Time) time.Time { return n.Add(time.Hour) }},
	{"in 3 hours", func(n time.Time) time.Time { return n.Add(3 * time.Hour) }},
	{"tomorrow at 9", func(n time.Time) time.Time { return nineOn(n.AddDate(0, 0, 1)) }},
	{"next week", func(n time.Time) time.Time {
		d := (8 - int(n.Weekday())) % 7
		if d == 0 {
			d = 7
		}
		return nineOn(n.AddDate(0, 0, d))
	}},
}

func nineOn(t time.Time) time.Time {
	y, mo, d := t.Date()
	return time.Date(y, mo, d, 9, 0, 0, 0, t.Location())
}

// actKey is `.`, or one of the keys that only the menu had: u s p m.
func (m *Model) actKey(s string) tea.Cmd {
	msg, ok := m.selected()
	if !ok {
		return m.say("pick a message first (↑)", false)
	}
	if s == "." {
		return m.openMenu(msg)
	}
	return m.do(s, msg)
}

// do is the action on key, for msg in the open conversation (or its
// thread, while that's swapped in).
func (m *Model) do(key string, msg slack.Message) tea.Cmd {
	switch key {
	case "r":
		m.reactPicker(msg)
	case "t":
		return m.threadAt("t")
	case "a":
		return m.claudeAbout()
	case "s", "O":
		return m.save(key == "O")
	case "L":
		return m.toggleSave(m.open, msg)
	case "m":
		m.acts.menu = menu{on: true, remind: true, thread: m.th.in, conv: m.open, msg: msg}
	case "p":
		return m.togglePin(m.open, msg)
	case "u":
		return m.markUnread(msg)
	case "l":
		return tea.Batch(tea.SetClipboard(m.permalink(msg)), m.say("copied a link to the message", false))
	case "c":
		return m.copyText(msg)
	case "o":
		return m.openLink(msg)
	case "e":
		return m.edit(msg)
	case "d":
		return m.remove(msg)
	}
	return nil
}

// act runs call in the background, putting things back with undo if it
// fails. what is what it was, for the failure's flash.
func (m *Model) act(what string, call func() error, undo func()) tea.Cmd {
	return func() tea.Msg {
		err := call()
		if err != nil && undo != nil {
			undo()
		}
		return actedMsg{what: what, err: err}
	}
}

func (m *Model) acted(a actedMsg) tea.Cmd {
	switch {
	case a.again != nil:
		saved := false
		m.st.Read(func(v store.View) { saved = v.IsSaved(a.conv, a.again.TS) })
		return m.setSaved(a.conv, *a.again, !saved)
	case a.err != nil:
		return m.say("✗ couldn't "+a.what+": "+a.err.Error(), true)
	}
	return nil
}

// markUnread moves the read marker to the message before msg (a hair
// before it, if that isn't held), so msg and what follows are unread
// again, in the sidebar too. It stays so until the conversation is opened
// again.
func (m *Model) markUnread(msg slack.Message) tea.Cmd {
	if m.th.in {
		return m.say("mark unread is for the conversation's messages, not a thread's", false)
	}
	conv, ts := m.open, tsBefore(msg.TS)
	m.st.Read(func(v store.View) {
		if w := v.Window(conv); w != nil {
			if i := msgIndex(w.Msgs, msg.TS); i > 0 && i < len(w.Msgs) {
				ts = w.Msgs[i-1].TS
			}
		}
	})
	m.st.MarkUnread(conv, ts)
	m.acts.unread, m.newAt = conv, ts
	return tea.Batch(m.say("marked unread from here", false), m.act("mark it unread", func() error { return m.api.Mark(m.ctx, conv, ts) }, nil))
}

// tsBefore is a ts a microsecond earlier.
func tsBefore(ts string) string {
	t := tsTime(ts).Add(-time.Microsecond)
	return fmt.Sprintf("%d.%06d", t.Unix(), t.Nanosecond()/1000)
}

// toggleSave saves msg for later, or takes it off. Whether it's on the
// list is known once the list has been fetched, which this does first if
// it must.
func (m *Model) toggleSave(conv string, msg slack.Message) tea.Cmd {
	loaded, saved := false, false
	m.st.Read(func(v store.View) { loaded, saved = v.Loaded(store.LaterList), v.IsSaved(conv, msg.TS) })
	if !loaded {
		return func() tea.Msg {
			_ = m.st.Fetch(m.ctx, m.api, store.LaterList) // not fetched: it's taken as not saved
			return actedMsg{again: &msg, conv: conv}
		}
	}
	return m.setSaved(conv, msg, !saved)
}

func (m *Model) setSaved(conv string, msg slack.Message, save bool) tea.Cmd {
	if save {
		m.st.SaveLater(conv, msg, 0)
		return tea.Batch(m.say("saved for later", false), m.act("save it", func() error {
			return m.api.SaveMessage(m.ctx, conv, msg.TS, 0)
		}, func() { m.st.Unsave(conv, msg.TS) }))
	}
	m.st.Unsave(conv, msg.TS)
	return tea.Batch(m.say("taken off later", false), m.act("take it off later", func() error {
		return m.api.Unsave(m.ctx, "message", conv, msg.TS)
	}, nil))
}

// remindAt saves msg for later due when choice i says, which is what
// Slack's own "remind me about this" is.
func (m *Model) remindAt(conv string, msg slack.Message, i int) tea.Cmd {
	c := remindChoices[i]
	due := c.at(time.Now())
	m.st.SaveLater(conv, msg, due.Unix())
	return tea.Batch(m.say("saved for later, due "+clock(due, time.Now()), false), m.act("remind you", func() error {
		return m.api.SaveMessage(m.ctx, conv, msg.TS, due.Unix())
	}, func() { _ = m.st.Fetch(m.ctx, m.api, store.LaterList) }))
}

// togglePin pins msg, or unpins it.
func (m *Model) togglePin(conv string, msg slack.Message) tea.Cmd {
	pin := !slices.Contains(msg.PinnedTo, conv)
	m.st.SetPinned(conv, msg.TS, pin)
	what, said := "pin it", "pinned"
	if !pin {
		what, said = "unpin it", "unpinned"
	}
	return tea.Batch(m.say(said, false), m.act(what, func() error {
		err := m.api.Pin(m.ctx, conv, msg.TS, pin)
		var e *slack.Error
		if errors.As(err, &e) && (e.Code == "already_pinned" || e.Code == "no_pin") {
			err = nil // already how you wanted it
		}
		return err
	}, func() { m.st.SetPinned(conv, msg.TS, !pin) }))
}

// --- what's drawn ---

// copyW is the width a message is laid out at to be copied or searched
// for links: wide, so nothing wraps that wasn't a line of its own.
const copyW = 240

// asDrawn is msg's body as it's drawn, blocks, attachments and files
// included, less its header and reactions.
func (m *Model) asDrawn(v store.View, msg *slack.Message) []canvas.Row {
	rows := bodyRows(&m.pal, v, msg, copyW)
	rows = append(rows, attachmentRows(&m.pal, v, msg.Attachments, copyW, time.Now())...)
	return append(rows, fileRows(&m.pal, msg.Files, copyW)...)
}

// codeGap is a gap in a code line that can only be the language's name,
// set at the far right.
const codeGap = "                    "

// rowsText is rows as plain text: the bars that set attachments and
// quotes apart gone (quotes as ">"), pictures blank, and trailing space
// trimmed.
func (m *Model) rowsText(rows []canvas.Row) string {
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		var b strings.Builder
		code := false
		for _, s := range r {
			switch s.Text {
			case "▌ ":
			case "▏ ":
				b.WriteString("> ")
			default:
				b.WriteString(termimg.Blank(s.Text))
			}
			code = code || s.St.BG == m.pal.Panel.BG
		}
		line := strings.TrimRight(b.String(), " ")
		if code {
			line = strings.TrimPrefix(line, " ")
			if i := strings.LastIndex(line, codeGap); i >= 0 && !strings.Contains(strings.TrimLeft(line[i:], " "), " ") {
				line = strings.TrimRight(line[:i], " ")
			}
		}
		lines = append(lines, line)
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

// webLinks is the links in rows that are safe to open, in order: http,
// https and mailto only, since messages are from anyone and other schemes
// can start apps.
func webLinks(rows []canvas.Row) []string {
	var out []string
	for _, r := range rows {
		for _, s := range r {
			u, err := url.Parse(s.St.Link)
			if s.St.Link == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") {
				continue
			}
			if t := u.String(); !slices.Contains(out, t) {
				out = append(out, t)
			}
		}
	}
	return out
}

func (m *Model) copyText(msg slack.Message) tea.Cmd {
	var text string
	m.st.Read(func(v store.View) { text = m.rowsText(m.asDrawn(v, &msg)) })
	if text == "" {
		return m.say("nothing in that one to copy", false)
	}
	return tea.Batch(tea.SetClipboard(text), m.say("copied the message", false))
}

// openLink opens the first web link in msg, as drawn, in the browser.
func (m *Model) openLink(msg slack.Message) tea.Cmd {
	var links []string
	m.st.Read(func(v store.View) { links = webLinks(m.asDrawn(v, &msg)) })
	if len(links) == 0 {
		return m.say("no link in that one", false)
	}
	target := links[0]
	return tea.Batch(m.say("opening "+target, false), func() tea.Msg {
		if err := exec.Command("open", target).Run(); err != nil {
			slog.Warn("open link", "err", err)
		}
		return nil
	})
}
