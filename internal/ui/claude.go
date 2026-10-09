package ui

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/mrkdwn"
	"github.com/0xdeafcafe/loafer/internal/rushlink"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
)

// The claude pane (docs/claude-pane.md): slackbot's ai, done by your own
// agent through rush. Chips say what slack text goes with the next
// message, the answer streams in as markdown, follow-ups go to the same
// session, and an answer only ever becomes a draft. It sends nothing to
// Slack.

type claude struct {
	link   rushlink.Link // nil without rush
	looked bool          // rush has been looked for

	srcs    []*source // the chips
	conv    string    // the conversation answers are drafts for
	input   []rune
	cur     int
	typing  bool // the box has the keys, else the answer does
	starter int  // the starter picked, before anything's asked

	id     string // the session's uuid; "" until the first ask
	turns  []turn
	busy   bool
	events <-chan rushlink.Event
	stop   context.CancelFunc
	scroll int // rows up from the newest
}

// turn is a message to the agent and what came back.
type turn struct {
	ask    string
	chips  []string
	said   []string // each message the agent finished
	words  string   // the one it's still saying, from delta events
	result string
	steps  []string // the tools it used, in words
	state  string   // "" working, "done", "failed"
	note   string   // what it's doing, blocked on, or what went wrong
	cost   float64

	drawn  []canvas.Row // the answer laid out, for drawnW cells and drawnN bytes
	drawnW int
	drawnN int
}

func (t *turn) answer() string {
	if len(t.said) == 0 && t.words == "" {
		return t.result
	}
	return strings.TrimSpace(strings.Join(append(slices.Clip(t.said), t.words), "\n\n"))
}

type (
	askedMsg struct {
		id    string
		first bool
		ch    <-chan rushlink.Event
		err   error
	}
	claudeMsg struct {
		ch <-chan rushlink.Event
		e  rushlink.Event
		ok bool
	}
	caughtUpMsg struct{ convs []string }
	rushDoneMsg struct{ err error }
)

const rushHow = "Claude needs rush · go install github.com/0xdeafcafe/rush/cmd/rush@latest"

// claudeOpened is the Claude tab coming up: rush is looked for the first
// time, and the open conversation is attached if nothing is.
func (m *Model) claudeOpened() {
	c := &m.claude
	c.typing = true
	if c.link == nil && !c.looked {
		c.looked = true
		if l, err := rushlink.Find(); err == nil {
			c.link = l
		}
	}
	if len(c.srcs) == 0 && len(c.turns) == 0 && m.open != "" {
		m.st.Read(func(v store.View) { c.attach(convSource(&m.pal, v, m.open, time.Now())) })
		c.conv = m.open
	}
}

// openClaude goes to the Claude tab with s as its chip.
func (m *Model) openClaude(s *source) tea.Cmd {
	c := &m.claude
	c.attach(s)
	c.conv, c.starter = s.conv, 0
	return m.setTab(tabClaude)
}

// claudeHere opens the pane on the open conversation since you last read.
func (m *Model) claudeHere() tea.Cmd {
	var s *source
	m.st.Read(func(v store.View) { s = convSource(&m.pal, v, m.open, time.Now()) })
	if s == nil {
		return m.say("open a conversation first", false)
	}
	return m.openClaude(s)
}

// claudeAbout opens the pane on the selected message and those round it.
func (m *Model) claudeAbout() tea.Cmd {
	var s *source
	m.st.Read(func(v store.View) {
		switch w := m.window(v); {
		case w != nil && m.th.in:
			s = threadSource(&m.pal, v, m.open, w.Msgs) // in a thread, the whole of it
		case w != nil:
			s = aroundSource(&m.pal, v, m.open, w.Msgs, msgIndex(w.Msgs, m.sel), 5)
		}
	})
	if s == nil {
		return m.say("pick a message first (↑)", false)
	}
	return m.openClaude(s)
}

// attach makes srcs the chips, cut to fit a prompt together.
func (c *claude) attach(srcs ...*source) {
	c.srcs = slices.DeleteFunc(srcs, func(s *source) bool { return s == nil })
	fit(c.srcs, promptCap)
}

func (c *claude) labels() []string {
	var out []string
	for _, s := range c.srcs {
		out = append(out, s.what+" · "+size(int64(len(s.text()))))
	}
	return out
}

// starters are the jobs on offer before anything's asked.
func (m *Model) starters() [][2]string {
	out := [][2]string{{"catch me up", "catchup"}}
	if conv := m.claude.conv; conv != "" {
		name := ""
		m.st.Read(func(v store.View) {
			if c := v.Conv(conv); c != nil {
				name = convName(v, c)[1]
			}
		})
		out = append([][2]string{{"summarise " + name + " since you last read", "summarise"}, {"draft a reply in " + name, "draft"}}, out...)
	}
	return out
}

const (
	askSummarise = "summarise this: what happened, what was decided, and what's waiting on you. plain text, short."
	askDraft     = "draft my reply to this conversation, in my tone (the samples are mine, for tone only). answer with only the message, ready to paste into slack, nothing round it."
	askCatchUp   = "catch me up: a line or two for each conversation on what happened and anything waiting on you, mentions and dms first. plain text, short."
)

// runStarter does job: summarise, draft or catchup.
func (m *Model) runStarter(label, job string) tea.Cmd {
	c := &m.claude
	switch job {
	case "summarise":
		if len(c.srcs) == 0 {
			m.st.Read(func(v store.View) { c.attach(convSource(&m.pal, v, c.conv, time.Now())) })
		}
		return m.claudeAsk(label, askSummarise, c.srcs)
	case "draft":
		srcs := c.srcs
		m.st.Read(func(v store.View) {
			if len(srcs) == 0 {
				if w := v.Window(c.conv); w != nil && len(w.Msgs) > 0 {
					s := aroundSource(&m.pal, v, c.conv, w.Msgs, len(w.Msgs)-1, 29)
					s.attrs = s.attrs[:1]
					s.what = fmt.Sprintf("%s · the last %d messages", s.attrs[0][1], len(s.lines))
					srcs = []*source{s}
				}
			}
			srcs = append(slices.Clip(srcs), samplesSource(&m.pal, v, m.sideConvs(), 40))
		})
		c.attach(srcs...)
		return m.claudeAsk(label, askDraft, c.srcs)
	}
	return m.catchUp()
}

// catchUp fetches the unread conversations not held yet, then asks about
// all of them, mentions and dms first.
func (m *Model) catchUp() tea.Cmd {
	c := &m.claude
	var convs []string
	m.st.Read(func(v store.View) {
		for _, first := range []bool{true, false} {
			for _, id := range m.sideConvs() {
				if cv := v.Conv(id); cv != nil && unreadConv(cv) && needsYou(cv) == first {
					convs = append(convs, id)
				}
			}
		}
	})
	convs = convs[:min(len(convs), 15)] // the store holds 20 windows
	if len(convs) == 0 {
		return m.say("nothing unread to catch up on", false)
	}
	c.srcs = nil
	c.turns = append(c.turns, turn{ask: "catch me up", note: fmt.Sprintf("reading %d conversations", len(convs))})
	c.busy, c.typing, c.scroll = true, false, 0
	st, api, ctx := m.st, m.api, m.ctx
	return func() tea.Msg {
		for _, id := range convs {
			held := false
			st.Read(func(v store.View) { held = v.Window(id) != nil })
			if !held && api != nil {
				if err := st.Open(ctx, api, id); err != nil {
					slog.Warn("claude catch up", "conv", id, "err", err)
				}
			}
		}
		return caughtUpMsg{convs}
	}
}

func (m *Model) caughtUp(convs []string) tea.Cmd {
	var srcs []*source
	m.st.Read(func(v store.View) {
		for _, id := range convs {
			if s := convSource(&m.pal, v, id, time.Now()); s != nil && len(s.lines) > 0 {
				if n := len(s.lines) - 60; n > 0 {
					s.lines, s.left = s.lines[n:], s.left+n
				}
				srcs = append(srcs, s)
			}
		}
	})
	fit(srcs, promptCap)
	c := &m.claude
	t := &c.turns[len(c.turns)-1]
	for _, s := range srcs {
		t.chips = append(t.chips, s.what)
	}
	t.note = ""
	return m.claudeSend(askCatchUp, srcs)
}

// claudeAsk puts up a turn for what's asked and sends it, with srcs.
func (m *Model) claudeAsk(ask, instead string, srcs []*source) tea.Cmd {
	c := &m.claude
	c.turns = append(c.turns, turn{ask: ask, chips: c.labels()})
	c.busy, c.typing, c.scroll = true, false, 0
	return m.claudeSend(cmp.Or(instead, ask), srcs)
}

// claudeSend sends text and srcs to the session, starting it if there
// isn't one yet; the last turn shows what comes back.
func (m *Model) claudeSend(text string, srcs []*source) tea.Cmd {
	c := &m.claude
	var body string
	meta := map[string]string{"app": "loafer"} // as kanban tags a session with its card
	m.st.Read(func(v store.View) {
		body = prompt(v, srcs, text)
		meta["team"] = v.Team().ID
	})
	meta["conv"] = c.conv
	for k, v := range meta {
		if v == "" {
			delete(meta, k)
		}
	}
	first := c.id == ""
	if first {
		c.id = rushlink.NewID()
	}
	s := rushlink.Session{ID: c.id, Name: "loafer: " + cut(c.turns[len(c.turns)-1].ask, 60), Meta: meta}
	c.srcs, c.input, c.cur = nil, nil, 0
	if c.stop != nil {
		c.stop()
	}
	ctx, stop := context.WithCancel(m.ctx)
	c.stop = stop
	slog.Info("claude ask", "session", rushlink.Short(c.id), "first", first, "sources", len(srcs), "bytes", len(body)) // never the text
	link := c.link
	return func() tea.Msg {
		ch, err := rushlink.Ask(ctx, link, s, first, body)
		return askedMsg{s.ID, first, ch, err}
	}
}

func cut(s string, n int) string {
	s = firstLine(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func waitClaude(ch <-chan rushlink.Event) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-ch
		return claudeMsg{ch, e, ok}
	}
}

// claudeUpdate takes the pane's messages. Events arrive one Cmd at a
// time, so nothing runs or draws between them.
func (m *Model) claudeUpdate(msg tea.Msg) tea.Cmd {
	c := &m.claude
	switch msg := msg.(type) {
	case askedMsg:
		if msg.id != c.id || len(c.turns) == 0 {
			return nil // from a session alt+n left
		}
		if msg.err != nil {
			if msg.first {
				c.id = ""
			}
			c.fail(msg.err.Error())
			return nil
		}
		c.events = msg.ch
		return waitClaude(msg.ch)
	case claudeMsg:
		if msg.ch != c.events || len(c.turns) == 0 {
			return nil
		}
		t := &c.turns[len(c.turns)-1]
		if !msg.ok {
			if t.state == "" {
				c.fail("rush stopped before the answer came")
			}
			c.busy, c.events = false, nil
			return nil
		}
		c.apply(msg.e)
		return waitClaude(msg.ch)
	case caughtUpMsg:
		if c.busy && len(c.turns) > 0 && c.turns[len(c.turns)-1].ask == "catch me up" {
			return m.caughtUp(msg.convs)
		}
	case rushDoneMsg:
		if msg.err != nil {
			return m.say("couldn't open it in rush: "+msg.err.Error(), true)
		}
	}
	return nil
}

func (c *claude) fail(why string) {
	t := &c.turns[len(c.turns)-1]
	t.state, t.note = "failed", why
	c.busy = false
}

// apply is one event from rush on the turn under way.
func (c *claude) apply(e rushlink.Event) {
	t := &c.turns[len(c.turns)-1]
	switch e.Type {
	case "info":
		t.note = e.Detail
		if e.State == "blocked" {
			t.note = "Claude wants to use " + cmp.Or(e.Needs, "a tool") + " · ctrl+o to answer in rush"
		}
		t.cost = max(t.cost, e.CostUSD)
	case "delta":
		t.words += e.Text
	case "text":
		t.said, t.words = append(t.said, e.Text), ""
	case "tool":
		t.steps = append(t.steps, strings.TrimSpace(e.Name+" "+e.Doing))
	case "result":
		t.result, t.cost, t.note = e.Text, e.CostUSD, ""
		t.state = "done"
		if e.IsError {
			t.state, t.note = "failed", e.Text
		}
		c.busy = false
	case "closed", "failed":
		if t.state == "" {
			c.fail(cmp.Or(e.Text, "the session closed"))
		}
	}
}

// --- keys ---

func (m *Model) claudeKey(k tea.KeyPressMsg) tea.Cmd {
	c := &m.claude
	s := k.String()
	switch s {
	case "esc": // the box, then the answer, then home
		if c.typing && len(c.turns) > 0 {
			c.typing = false
			return nil
		}
		return m.setTab(tabHome)
	case "alt+n":
		if c.stop != nil {
			c.stop()
		}
		c.id, c.turns, c.busy, c.events, c.scroll, c.typing = "", nil, false, nil, 0, true
		return nil
	case "ctrl+o":
		if c.link == nil || c.id == "" {
			return m.say("nothing to open in rush yet", false)
		}
		return tea.ExecProcess(c.link.Open(c.id), func(err error) tea.Msg { return rushDoneMsg{err} })
	case "pgup":
		c.scroll += max(1, m.h/2)
		return nil
	case "pgdown":
		c.scroll = max(0, c.scroll-m.h/2)
		return nil
	}
	if c.link == nil {
		return nil
	}
	if !c.typing {
		switch s {
		case "i":
			return m.claudeInsert()
		case "up", "k":
			c.scroll++
		case "down", "j":
			c.scroll = max(0, c.scroll-1)
		case "enter":
			c.typing = true
		default:
			if k.Text != "" {
				c.typing = true
				c.insert(k.Text)
			}
		}
		return nil
	}
	fresh := len(c.turns) == 0 && len(c.input) == 0
	switch s {
	case "enter":
		if c.busy {
			return nil
		}
		if fresh {
			st := m.starters()
			pick := st[min(c.starter, len(st)-1)]
			return m.runStarter(pick[0], pick[1])
		}
		if ask := strings.TrimSpace(string(c.input)); ask != "" {
			return m.claudeAsk(ask, "", c.srcs)
		}
	case "up", "down":
		if fresh {
			n := len(m.starters())
			c.starter = (c.starter + map[string]int{"up": n - 1, "down": 1}[s]) % n
		} else if s == "up" {
			c.scroll++
		} else {
			c.scroll = max(0, c.scroll-1)
		}
	case "shift+enter", "alt+enter", "ctrl+j":
		c.insert("\n")
	case "backspace":
		if c.cur == 0 && len(c.srcs) > 0 {
			c.srcs = c.srcs[:len(c.srcs)-1]
		} else if c.cur > 0 {
			c.input = slices.Delete(c.input, c.cur-1, c.cur)
			c.cur--
		}
	case "ctrl+w", "alt+backspace":
		i := c.cur
		for i > 0 && blank(c.input[i-1]) {
			i--
		}
		for i > 0 && !blank(c.input[i-1]) {
			i--
		}
		c.input, c.cur = slices.Delete(c.input, i, c.cur), i
	case "ctrl+u":
		c.input, c.cur = c.input[c.cur:], 0
	case "left":
		c.cur = max(0, c.cur-1)
	case "right":
		c.cur = min(len(c.input), c.cur+1)
	case "home", "ctrl+a":
		c.cur = 0
	case "end", "ctrl+e":
		c.cur = len(c.input)
	default:
		if k.Text != "" {
			c.insert(k.Text)
		}
	}
	return nil
}

func (c *claude) insert(s string) {
	r := []rune(s)
	c.input = slices.Insert(c.input, c.cur, r...)
	c.cur += len(r)
}

// claudeInsert puts the last answer into its conversation's composer,
// after anything already there, and goes to it. It never sends.
func (m *Model) claudeInsert() tea.Cmd {
	c := &m.claude
	if len(c.turns) == 0 || c.turns[len(c.turns)-1].state != "done" {
		return m.say("no answer to insert yet", false)
	}
	text := strings.TrimSpace(c.turns[len(c.turns)-1].answer())
	conv := cmp.Or(c.conv, m.open)
	if conv == "" || text == "" {
		return m.say("open a conversation for the draft first", false)
	}
	cmd := m.setTab(tabHome)
	if conv != m.open {
		cmd = tea.Batch(cmd, m.visit(conv))
	} else if m.editing != "" {
		m.cancelEdit()
	}
	m.cur = len(m.input)
	if len(m.input) > 0 {
		text = "\n" + text
	}
	m.insert(text)
	m.setFocus(onCompose)
	return tea.Batch(cmd, m.say("the answer's in the box as a draft; it's yours to send", false))
}

// --- drawing ---

// claudePane is the pane, w by h: a greeting and starters before
// anything's asked, then the conversation; the chips and the box below.
func (m *Model) claudePane(v store.View, w, h int) []canvas.Row {
	ink := m.pal.Main
	c := &m.claude
	out := make([]canvas.Row, 0, h)
	cw := min(w-4, 96)
	pad := canvas.T(strings.Repeat(" ", max(0, (w-cw)/2)), ink.Text)
	put := func(r canvas.Row) { out = append(out, canvas.Fit(append(canvas.Row{pad}, r...), w, ink.Text)) }
	centre := func(r canvas.Row) {
		out = append(out, canvas.Fit(append(canvas.Row{canvas.T(strings.Repeat(" ", max(0, (w-r.Width())/2)), ink.Text)}, r...), w, ink.Text))
	}
	fill := func(n int) {
		for len(out) < n {
			out = append(out, canvas.Fit(nil, w, ink.Text))
		}
	}
	if c.link == nil {
		fill(h / 2)
		centre(canvas.Row{canvas.T(rushHow, ink.Dim)})
		fill(h)
		return out[:h]
	}
	box := m.claudeBox(cw)
	if len(c.turns) == 0 {
		name, _, _ := strings.Cut(v.Person(v.Self()).Name, " ")
		fill(max(1, h/4))
		centre(canvas.Row{canvas.T("✻ ", m.pal.Orange), canvas.T("Hi "+name+". Ask about your Slack.", ink.Bright)})
		fill(len(out) + 1)
		for _, r := range box {
			put(r)
		}
		fill(len(out) + 1)
		for i, st := range m.starters() {
			mark, text := canvas.T("  ◇ ", ink.Dim), ink.Sub
			if i == c.starter && c.typing && len(c.input) == 0 {
				mark, text = canvas.T("▍ ◇ ", m.pal.Orange), ink.Bright
			}
			put(canvas.Row{mark, canvas.T(st[0], text)})
		}
		fill(h)
		return out[:h]
	}
	var talk []canvas.Row
	for i := range c.turns {
		talk = append(talk, m.turnRows(v, &c.turns[i], cw)...)
	}
	room := max(0, h-len(box)-1)
	c.scroll = min(c.scroll, max(0, len(talk)-room))
	end := len(talk) - c.scroll
	talk = talk[max(0, end-room):end]
	fill(room - len(talk))
	for _, r := range talk {
		put(r)
	}
	fill(h - len(box))
	for _, r := range box {
		put(r)
	}
	return out[:h]
}

// turnRows is a turn as rush draws a session: the ask, then the answer
// behind a ▏ spine, its steps folded, and how it ended.
func (m *Model) turnRows(v store.View, t *turn, w int) []canvas.Row {
	ink := m.pal.Main
	out := []canvas.Row{nil}
	for i, l := range canvas.Wrap(canvas.Row{canvas.T(t.ask, ink.Bright)}, w-2) {
		lead := canvas.T("  ", ink.Text)
		if i == 0 {
			lead = canvas.T("❯ ", m.pal.Orange.With(canvas.Bold))
		}
		out = append(out, append(canvas.Row{lead}, l...))
	}
	for _, ch := range t.chips {
		out = append(out, canvas.Row{canvas.T("  with "+ch, ink.Dim)})
	}
	out = append(out, nil)
	spine := canvas.T("▏ ", ink.Faint)
	if n := len(t.steps); n > 0 {
		step := fmt.Sprintf("▸ %d steps", n)
		if n == 1 {
			step = "▸ 1 step"
		}
		if t.state == "" {
			step = "· " + t.steps[n-1]
		}
		out = append(out, canvas.Row{spine, canvas.T(step, ink.Dim)})
	}
	if a := t.answer(); a != "" {
		if t.drawnW != w || t.drawnN != len(a) {
			t.drawn, t.drawnW, t.drawnN = textRows(&m.pal, v, mrkdwn.Parse(a), w-2), w, len(a)
		}
		for _, r := range t.drawn {
			out = append(out, append(canvas.Row{spine}, r...))
		}
	}
	var end canvas.Row
	switch {
	case t.state == "done":
		end = canvas.Row{canvas.T("✓", m.pal.Green)}
	case t.state == "failed":
		end = canvas.Row{canvas.T("✗ "+t.note, m.pal.Red)}
	case strings.HasPrefix(t.note, "Claude wants"):
		end = canvas.Row{canvas.T("✻ ", m.pal.Orange), canvas.T(t.note, m.pal.Yellow)}
	default:
		end = canvas.Row{canvas.T("✻ ", m.pal.Orange), canvas.T(cmp.Or(t.note, "working"), ink.Dim)}
	}
	if t.cost > 0 && t.state != "" {
		end = rightAlign(end, canvas.Row{canvas.T(fmt.Sprintf("$%.2f", t.cost), ink.Dim)}, w, ink.Text)
	}
	return append(out, end)
}

// claudeBox is the chips, then rush's input box, w wide.
func (m *Model) claudeBox(w int) []canvas.Row {
	ink := m.pal.Main
	c := &m.claude
	var out []canvas.Row
	for _, l := range c.labels() {
		out = append(out, canvas.Row{canvas.T(cut(l, w-2)+" ✕", ink.Sub.Bg(m.pal.Chip.BG))})
	}
	edge := ink.Edge
	if c.typing {
		edge = m.pal.Orange
	}
	label := func(l, left, right, r string) canvas.Row {
		row := canvas.Row{canvas.T(l+" ", edge), canvas.T(left, ink.Dim)}
		tail := canvas.Row{canvas.T(r, edge)}
		if right != "" {
			tail = append(canvas.Row{canvas.T(" "+right+" ", ink.Dim)}, tail...)
		}
		return append(append(row, canvas.T(" "+strings.Repeat("─", max(0, w-row.Width()-tail.Width()-1)), edge)), tail...)
	}
	out = append(out, label("╭", "to Claude, through rush", "plan", "╮"))
	field := m.pal.Input
	inner := max(4, w-6)
	var text canvas.Row
	switch {
	case len(c.input) == 0 && c.typing:
		text = canvas.Row{canvas.T(" ", field.Bg(ink.Text.FG)), canvas.T("ask about what's above, or anything", field.Fg(ink.Faint.FG))}
	case len(c.input) == 0:
		text = canvas.Row{canvas.T("ask more", field.Fg(ink.Faint.FG))}
	case c.typing:
		at, rest := " ", ""
		if c.cur < len(c.input) {
			at, rest = string(c.input[c.cur]), string(c.input[c.cur+1:])
			if at == "\n" {
				at, rest = " ", "\n"+rest
			}
		}
		text = canvas.Row{canvas.T(string(c.input[:c.cur]), field), canvas.T(at, field.Bg(ink.Text.FG).Fg(field.BG)), canvas.T(rest, field)}
	default:
		text = canvas.Row{canvas.T(string(c.input), field)}
	}
	var lines []canvas.Row
	for _, l := range splitLines(text) {
		lines = append(lines, canvas.Wrap(l, inner)...)
	}
	lines = lines[max(0, len(lines)-4):] // shortcut: follows the end, not the cursor
	for i, l := range lines {
		lead := "  "
		if i == 0 {
			lead = "❯ "
		}
		row := canvas.Row{canvas.T("│", edge), canvas.T(" ", field), canvas.T(lead, field.Fg(m.pal.Orange.FG).With(canvas.Bold))}
		out = append(out, append(append(row, canvas.Fit(l, inner, field)...), canvas.T(" ", field), canvas.T("│", edge)))
	}
	hint := "runs on your own agents · ctrl+o open in rush"
	if n := len(c.turns); n > 0 && c.turns[n-1].state == "done" && !c.typing {
		hint = "i insert as draft · ctrl+o open in rush"
	}
	return append(out, label("╰", hint, "", "╯"))
}

// claudeHints is the hint line's pairs while the pane's up.
func (m *Model) claudeHints() [][2]string {
	c := &m.claude
	switch {
	case c.link == nil:
		return [][2]string{{"esc", "back"}}
	case !c.typing:
		return [][2]string{{"i", "insert as draft"}, {"enter", "ask more"}, {"↑↓", "scroll"}, {"alt+n", "new"}, {"ctrl+o", "open in rush"}, {"esc", "home"}}
	case len(c.turns) == 0 && len(c.input) == 0:
		return [][2]string{{"↑↓", "choose"}, {"enter", "ask"}, {"backspace", "drop a chip"}, {"esc", "home"}}
	}
	return [][2]string{{"enter", "ask"}, {"backspace", "drop a chip"}, {"alt+n", "new"}, {"ctrl+o", "open in rush"}, {"esc", "the answer"}}
}

// redraw lays the answers out again, after the palette or pictures change.
func (c *claude) redraw() {
	for i := range c.turns {
		c.turns[i].drawnW = 0
	}
}
