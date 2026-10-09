package ui

import (
	"context"
	"log/slog"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/notify"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
)

// Notifications and the typing line. The store decides what notifies
// (internal/notify); this is only the terminal's side: whether it has
// focus, holding a burst back, and showing the note.

type (
	noteMsg   notify.Note
	flushMsg  struct{}
	typingMsg struct{}
)

type alerts struct {
	blurred  bool // the terminal has lost focus
	throttle notify.Throttle
	flushing bool // a flushMsg is on its way
	typing   bool // a typingMsg is on its way
}

func (m *Model) waitNotes() tea.Cmd {
	return func() tea.Msg {
		select {
		case n := <-m.st.Notes():
			return noteMsg(n)
		case <-m.ctx.Done():
			return nil
		}
	}
}

// watch tells the store what's on screen, so it can leave out what you're
// looking at.
func (m *Model) watch() { m.st.Watch(m.open, !m.al.blurred) }

func (m *Model) alert(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.FocusMsg, tea.BlurMsg:
		_, m.al.blurred = msg.(tea.BlurMsg)
		m.watch()
	case noteMsg:
		n, ok := m.al.throttle.Push(notify.Note(msg), time.Now())
		if ok {
			return tea.Batch(m.waitNotes(), m.deliver(n))
		}
		cmd := m.waitNotes()
		if !m.al.flushing {
			m.al.flushing = true
			cmd = tea.Batch(cmd, tea.Tick(m.al.throttle.Wait(time.Now()), func(time.Time) tea.Msg { return flushMsg{} }))
		}
		return cmd
	case flushMsg:
		m.al.flushing = false
		if n, ok := m.al.throttle.Flush(time.Now()); ok {
			return m.deliver(n)
		}
	case typingMsg:
		m.al.typing = false
		return m.watchTyping()
	}
	return nil
}

// deliver shows n by the terminal's own notification if it has one, else
// through macOS, off the UI goroutine.
func (m *Model) deliver(n notify.Note) tea.Cmd {
	if esc := notify.Escape(os.Getenv, n); esc != "" {
		return tea.Raw(esc)
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
		defer cancel()
		if err := notify.Osascript(ctx, n); err != nil {
			slog.Warn("notify", "err", err)
		}
		return nil
	}
}

// watchTyping arms the one tick that clears the typing line when the
// open conversation's first typist lapses. It does nothing when nobody's
// typing, or when it's armed already.
func (m *Model) watchTyping() tea.Cmd {
	if m.al.typing || m.open == "" {
		return nil
	}
	var until time.Time
	m.st.Read(func(v store.View) { _, until = v.Typing(m.open, time.Now()) })
	if until.IsZero() {
		return nil
	}
	m.al.typing = true
	return tea.Tick(time.Until(until)+time.Millisecond, func(time.Time) tea.Msg { return typingMsg{} })
}

// typingRow is "Alex is typing…" for the open conversation, or nil.
func (m *Model) typingRow(v store.View, w int) []canvas.Row {
	ids, _ := v.Typing(m.open, time.Now())
	if len(ids) == 0 {
		return nil
	}
	var text string
	switch len(ids) {
	case 1:
		text = v.Person(ids[0]).Name + " is typing…"
	case 2:
		text = v.Person(ids[0]).Name + " and " + v.Person(ids[1]).Name + " are typing…"
	default:
		text = "several people are typing…"
	}
	ink := m.pal.Main
	return []canvas.Row{canvas.Fit(canvas.Row{canvas.T("  "+text, ink.Dim.With(canvas.Italic))}, w, ink.Text)}
}
