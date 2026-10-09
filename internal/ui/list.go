package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/cellw"
)

// The open conversation is drawn a screen at a time, as rush draws a
// transcript: m.index counts every message's rows (as last drawn, else at
// an estimate), so the messages on screen are found by halving and only
// they're drawn, at the newest or the oldest alike. Drawings are kept up
// to keepRows, the least recently drawn going first.

const keepRows = 20000

// rowKey is a drawn message: which, how wide, and with a header or not.
// The fingerprint changes when anything drawn about it does.
type rowKey struct {
	ts, print string
	w         int
	header    bool
}

func fingerprint(msg *slack.Message) string {
	var b strings.Builder
	b.WriteString(strconv.Itoa(len(msg.Text)))
	b.WriteByte('/')
	b.WriteString(strconv.Itoa(len(msg.Blocks) + len(msg.Attachments) + len(msg.Files)))
	b.WriteByte('/')
	b.WriteString(strconv.Itoa(msg.ReplyCount))
	b.WriteString(msg.LatestReply)
	if msg.Edited != nil {
		b.WriteByte('e')
	}
	if len(msg.PinnedTo) > 0 {
		b.WriteByte('p')
	}
	for _, r := range msg.Reactions {
		b.WriteString(r.Name)
		b.WriteString(strconv.Itoa(r.Count))
	}
	return b.String()
}

// indexKey is what m.index was counted for; when it changes, it's
// counted again (from m.heights where a message has been drawn).
type indexKey struct {
	conv, first, last, newAt string
	n, w                     int
	names                    uint64
}

// dividers says what goes above msg: the "new" line, a day, and its own
// header (else it carries on prev's).
func (m *Model) dividers(v store.View, prev, msg *slack.Message) (newLine, dayLine, header bool) {
	newLine = m.newAt != "" && msg.TS > m.newAt && (prev == nil || prev.TS <= m.newAt) && msg.User != v.Self()
	dayLine = prev == nil || !sameDay(tsTime(prev.TS), tsTime(msg.TS))
	return newLine, dayLine, !followsOn(prev, msg) || newLine || dayLine
}

func before(msgs []slack.Message, i int) *slack.Message {
	if i > 0 {
		return &msgs[i-1]
	}
	return nil
}

// estimate is message i's rows before it's been drawn.
func (m *Model) estimate(v store.View, msgs []slack.Message, i, w int) int {
	msg := &msgs[i]
	newLine, dayLine, header := m.dividers(v, before(msgs, i), msg)
	n := strings.Count(msg.Text, "\n") + 1 + len(msg.Text)/max(1, w-gutter-1)
	for _, b := range []bool{header, header && i > 0, dayLine, newLine, len(msg.Reactions) > 0, msg.ReplyCount > 0} {
		if b {
			n++
		}
	}
	return n
}

// block is message i drawn w wide, with what goes above it.
func (m *Model) block(v store.View, msgs []slack.Message, i, w int, now time.Time) []canvas.Row {
	ink := m.pal.Main
	msg, prev := &msgs[i], before(msgs, i)
	newLine, dayLine, header := m.dividers(v, prev, msg)
	key := rowKey{msg.TS, fingerprint(msg), w, header}
	drawn, ok := m.drawn.Get(key)
	if !ok {
		drawn = renderMessage(&m.pal, v, msg, w-1, header, now)
		for j := range drawn {
			drawn[j] = canvas.Fit(append(canvas.Row{canvas.T(" ", ink.Text)}, drawn[j]...), w, ink.Text)
		}
		m.drawn.Put(key, drawn, len(drawn))
	}
	if msg.TS == m.sel && m.focus == onMsgs {
		drawn = m.picked(drawn)
	}
	var above []canvas.Row
	if newLine {
		rule := canvas.Row{canvas.T(strings.Repeat("─", max(0, w-6)), m.pal.Orange), canvas.T(" new ", m.pal.Orange.With(canvas.Bold))}
		above = append(above, canvas.Fit(rule, w, ink.Text))
	}
	if dayLine {
		label := " " + day(tsTime(msg.TS), now) + " "
		pad := max(0, (w-cellw.String(label))/2)
		above = append(above, canvas.Fit(canvas.Row{canvas.T(strings.Repeat(" ", pad), ink.Text), canvas.T(label, ink.Sub.With(canvas.Bold))}, w, ink.Text))
	}
	if header && prev != nil {
		above = append(above, canvas.Fit(nil, w, ink.Text))
	}
	if len(above) == 0 {
		return drawn
	}
	return append(above, drawn...)
}

// messages draws the open conversation's messages that fit h rows,
// scrolled up m.scroll rows from the newest, bringing the cursor's
// message into view first if it's just moved.
func (m *Model) messages(v store.View, c *store.Conv, w, h int) []canvas.Row {
	ink := m.pal.Main
	if w != m.rowsW || v.Names() != m.rowsNames {
		m.drawn.Clear()
		clear(m.heights)
		m.rowsW, m.rowsNames = w, v.Names()
	}
	out := make([]canvas.Row, 0, h)
	pad := func() []canvas.Row {
		for len(out) < h {
			out = append([]canvas.Row{canvas.Fit(nil, w, ink.Text)}, out...)
		}
		return out
	}
	win := m.window(v)
	if win == nil || len(win.Msgs) == 0 {
		note := "  loading…"
		if win != nil {
			note = "  nothing here yet"
		}
		m.scroll, m.follow = 0, false
		out = append(out, canvas.Fit(canvas.Row{canvas.T(note, ink.Dim)}, w, ink.Text))
		return pad()
	}
	msgs, now := win.Msgs, time.Now()

	key := indexKey{c.ID, msgs[0].TS, msgs[len(msgs)-1].TS, m.newAt, len(msgs), w, v.Names()}
	if key != m.indexOf {
		hs := make([]int, len(msgs))
		for i := range msgs {
			if n, ok := m.heights[msgs[i].TS]; ok {
				hs[i] = n
			} else {
				hs[i] = m.estimate(v, msgs, i, w)
			}
		}
		m.index.Reset(hs)
		m.indexOf = key
	}
	draw := func(i int) []canvas.Row {
		b := m.block(v, msgs, i, w, now)
		m.index.Set(i, len(b))
		m.heights[msgs[i].TS] = len(b)
		return b
	}

	// The cursor's message comes into view, what's above it included.
	if m.follow && m.sel != "" {
		if i := msgIndex(msgs, m.sel); i < len(msgs) && msgs[i].TS == m.sel {
			draw(i)
			below := m.index.Total() - m.index.Prefix(i+1) // rows under it
			if below < m.scroll {
				m.scroll = below
			}
			if top := below + m.index.Height(i); top > m.scroll+h {
				m.scroll = top - h
			}
		}
	}
	m.follow = false
	total := m.index.Total()
	m.scroll = min(max(0, m.scroll), max(0, total-h))

	// From the message holding the bottom row on screen, upward: hang is
	// how many of its rows are below the screen.
	s := m.index.Find(total - m.scroll - 1)
	hang := m.scroll - (total - m.index.Prefix(s+1))
	var blocks [][]canvas.Row
	got := 0
	for i := s; i >= 0 && got < h+hang; i-- {
		b := draw(i)
		blocks = append(blocks, b)
		got += len(b)
	}
	all := make([]canvas.Row, 0, got)
	for i := len(blocks) - 1; i >= 0; i-- {
		all = append(all, blocks[i]...)
	}
	end := max(0, len(all)-hang)
	out = append(out, all[max(0, end-h):end]...)
	m.drawn.Evict(nil)
	if len(m.heights) > 4*keepRows { // shortcut: forgets every height at once, fine at this size
		clear(m.heights)
	}
	return pad()
}
