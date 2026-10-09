package ui

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
)

// ctrl+f (or / in the messages) searches messages, as Slack's search
// does. What's typed goes to search.messages a quarter second after the
// typing stops, Slack's modifiers (in:#dev from:@drew before:2026-10-01
// is:thread) and all; tab narrows it to the open conversation. Each
// result is its conversation, who and when, then a line or two with the
// matched words lit. Enter goes to the message: picked out if it's held,
// else fetched with what's around it. Closing keeps the search for next
// time.

type finder struct {
	on    bool
	query []rune
	here  string // the conversation it's narrowed to, or ""
	gen   int    // goes up with every edit, so older ticks are dropped
	sent  string // the search asked for, which the results are for
	busy  bool
	err   string
	found []slack.Match
	page  int
	pages int
	total int
	at    int // the selected result
	top   int // the first result shown

	drawn      [][]canvas.Row // each result as drawn, unselected
	drawnW     int
	drawnNames uint64
}

type (
	searchTickMsg struct{ gen int }
	searchedMsg   struct {
		q    string
		page int
		res  slack.Found
		err  error
	}
	foundMsg struct {
		conv, ts string
		reply    string // the reply searched for, when ts is its thread's parent
		err      error
	}
)

const searchWait = 250 * time.Millisecond

// openSearch shows the search as it was left; a new one started from a
// conversation is narrowed to it, as Slack's ⌘F is.
func (m *Model) openSearch() {
	f := &m.find
	f.on = true
	if len(f.query) == 0 {
		f.here = m.open
	}
}

// searchFor searches everywhere for q, from ctrl+k.
func (m *Model) searchFor(q []rune) tea.Cmd {
	f := &m.find
	f.on, f.here, f.query = true, "", append(f.query[:0], q...)
	return m.edited()
}

// edited waits for the typing to stop: only the last edit's tick searches.
func (m *Model) edited() tea.Cmd {
	m.find.gen++
	gen := m.find.gen
	return tea.Tick(searchWait, func(time.Time) tea.Msg { return searchTickMsg{gen} })
}

// search is what goes to Slack: the query, narrowed if it's been.
func (f *finder) search() string {
	q := strings.TrimSpace(string(f.query))
	if q != "" && f.here != "" {
		q += " in:<#" + f.here + ">"
	}
	return q
}

func (m *Model) fetchSearch(q string, page int) tea.Cmd {
	return func() tea.Msg {
		res, err := m.api.Search(m.ctx, q, page)
		return searchedMsg{q, page, res, err}
	}
}

// searched takes the search's ticks and answers.
func (m *Model) searched(msg tea.Msg) tea.Cmd {
	f := &m.find
	switch msg := msg.(type) {
	case searchTickMsg:
		q := f.search()
		if msg.gen != f.gen || q == f.sent {
			return nil
		}
		f.sent, f.err, f.busy = q, "", q != ""
		if q == "" {
			f.found, f.drawn, f.page, f.pages, f.total, f.at, f.top = nil, nil, 0, 0, 0, 0, 0
			return nil
		}
		return m.fetchSearch(q, 1)
	case searchedMsg:
		if msg.q != f.sent || (msg.page > 1 && msg.page != f.page+1) {
			return nil // typed on since, or a page already had
		}
		f.busy = false
		if msg.err != nil {
			f.err = "couldn't search: " + msg.err.Error()
			if msg.page == 1 {
				f.sent, f.found, f.drawn = "", nil, nil // the next edit tries again
			}
			return nil
		}
		if msg.page == 1 {
			f.found, f.drawn, f.at, f.top = nil, nil, 0, 0
		}
		f.found = append(f.found, msg.res.Matches...)
		f.page, f.pages, f.total = msg.res.Page, msg.res.Pages, msg.res.Total
	case foundMsg:
		if msg.err != nil {
			return m.say("couldn't open: "+msg.err.Error(), true)
		}
		if msg.conv == m.open {
			return m.land(msg.conv, msg.ts, msg.reply)
		}
	}
	return nil
}

// more fetches the next page once the selection nears the end.
func (m *Model) more() tea.Cmd {
	f := &m.find
	if f.busy || f.sent == "" || f.page >= f.pages || f.at < len(f.found)-3 {
		return nil
	}
	f.busy = true
	return m.fetchSearch(f.sent, f.page+1)
}

func (m *Model) searchKey(k tea.KeyPressMsg) tea.Cmd {
	f := &m.find
	switch k.String() {
	case "esc", "ctrl+f":
		f.on = false
	case "enter":
		if f.at < len(f.found) {
			return m.goTo(f.found[f.at])
		}
	case "up", "ctrl+p":
		f.at = max(0, f.at-1)
	case "down", "ctrl+n":
		f.at = max(0, min(len(f.found)-1, f.at+1))
		return m.more()
	case "pgup":
		f.at = max(0, f.at-5)
	case "pgdown":
		f.at = max(0, min(len(f.found)-1, f.at+5))
		return m.more()
	case "tab":
		switch {
		case f.here != "":
			f.here = ""
		case m.open != "":
			f.here = m.open
		default:
			return nil
		}
		return m.edited()
	case "backspace":
		if len(f.query) == 0 {
			return nil
		}
		f.query = f.query[:len(f.query)-1]
		return m.edited()
	case "ctrl+u", "ctrl+w":
		f.query = f.query[:0]
		return m.edited()
	default:
		if k.Text == "" {
			return nil
		}
		f.query = append(f.query, []rune(k.Text)...)
		return m.edited()
	}
	return nil
}

// goTo opens x's conversation at x, or at the thread x is a reply in:
// at once if it's open and holds it, else after fetching what's around it.
func (m *Model) goTo(x slack.Match) tea.Cmd {
	conv, ts, reply := x.Channel.ID, x.TS, ""
	if p := x.Parent(); p != "" {
		ts, reply = p, x.TS
	}
	known, held := false, false
	m.st.Read(func(v store.View) { known, held = v.Conv(conv) != nil, v.Holds(conv, ts) })
	if !known {
		return m.say("that's in a conversation you're not in", false)
	}
	m.find.on = false
	fresh := conv != m.open
	_ = m.visit(conv) // its fetch is done below, before looking for ts
	if !fresh && held {
		return m.land(conv, ts, reply)
	}
	return func() tea.Msg {
		var err error
		if fresh {
			err = m.st.Open(m.ctx, m.api, conv)
		}
		if err == nil {
			held := false
			m.st.Read(func(v store.View) { held = v.Holds(conv, ts) })
			if !held {
				err = m.st.Around(m.ctx, m.api, conv, ts)
			}
		}
		return foundMsg{conv, ts, reply, err}
	}
}

// land puts the message cursor on ts. A reply's search lands on its
// parent; the thread pane opens on the reply here once there is one.
func (m *Model) land(conv, ts, reply string) tea.Cmd {
	held := false
	m.st.Read(func(v store.View) { held = v.Holds(conv, ts) })
	if !held {
		return m.say("that message has gone", false)
	}
	m.focus, m.sel, m.follow = onMsgs, ts, true
	_ = reply // threads: open the thread at reply here
	return nil
}

// --- drawing ---

// Slack's highlight marks, around each matched word.
const hlOn, hlOff = "", ""

var modifiers = []string{"in", "from", "to", "before", "after", "on", "during", "is", "has", "with"}

// modifier says whether t is one of Slack's search modifiers (in:#dev).
func modifier(t string) bool {
	k, _, ok := strings.Cut(t, ":")
	return ok && slices.Contains(modifiers, strings.ToLower(k))
}

// terms is the words of q to light where Slack didn't mark them.
func terms(q string) []string {
	var out []string
	for _, t := range strings.Fields(q) {
		if strings.HasPrefix(t, "-") || modifier(t) {
			continue
		}
		if t = strings.Trim(t, `"*`); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// mark puts Slack's highlight marks round each of words in text, any case.
func mark(text string, words []string) string {
	var b strings.Builder
	for i := 0; i < len(text); {
		n := 0
		for _, w := range words {
			if len(w) > n && i+len(w) <= len(text) && strings.EqualFold(text[i:i+len(w)], w) {
				n = len(w)
			}
		}
		if n == 0 {
			_, n = utf8.DecodeRuneInString(text[i:])
			b.WriteString(text[i : i+n])
		} else {
			b.WriteString(hlOn + text[i:i+n] + hlOff)
		}
		i += n
	}
	return b.String()
}

// snippet is text on one line, starting a little before its first match,
// in at most most rows of w, the matches in lit.
func snippet(text string, words []string, w, most int, base, lit canvas.Style) []canvas.Row {
	text = strings.Join(strings.Fields(text), " ")
	if !strings.Contains(text, hlOn) {
		text = mark(text, words)
	}
	if i := strings.Index(text, hlOn); i > 0 && utf8.RuneCountInString(text[:i]) > w/2 {
		r := []rune(text[:i])
		text = "…" + strings.TrimLeft(string(r[len(r)-w/4:]), " ") + text[i:]
	}
	var row canvas.Row
	st := base
	for text != "" {
		i := strings.IndexAny(text, hlOn+hlOff)
		if i < 0 {
			i = len(text)
		}
		if i > 0 {
			row = append(row, canvas.T(text[:i], st))
		}
		if i == len(text) {
			break
		}
		st = base
		if strings.HasPrefix(text[i:], hlOn) {
			st = lit
		}
		text = text[i+len(hlOn):]
	}
	rows := canvas.Wrap(row, w)
	if len(rows) > most {
		rows = rows[:most]
		rows[most-1] = append(canvas.Cut(rows[most-1], w-1), canvas.T("…", base))
	}
	return rows
}

// resultRows draws x w wide on fill: who, where and when, then the
// snippet. Each row starts with a two-cell gutter for the selection.
func (m *Model) resultRows(v store.View, x *slack.Match, words []string, w int, fill canvas.Style) []canvas.Row {
	ink := m.pal.Main
	where := "# " + x.Channel.Name
	switch c := v.Conv(x.Channel.ID); {
	case c != nil:
		where = convLabel(v, c)
	case x.Channel.IsIM:
		where = v.Person(x.Channel.Name).Name
	}
	who := x.Username
	if x.User != "" {
		who = v.Person(x.User).Name
	}
	head := canvas.Row{canvas.T("  ", fill), canvas.T(where, fill.Fg(m.pal.Blue.FG)), canvas.T("  ", fill),
		canvas.T(who, fill.Fg(ink.Bright.FG).With(canvas.Bold)), canvas.T("  "+clock(tsTime(x.TS), time.Now()), fill.Fg(ink.Dim.FG))}
	var tail canvas.Row
	if x.Parent() != "" {
		tail = canvas.Row{canvas.T("↩ in a thread ", fill.Fg(ink.Dim.FG))}
	}
	out := []canvas.Row{rightAlign(head, tail, w, fill)}
	lead := canvas.T("    ", fill)
	for _, r := range snippet(plainText(v, x.Text), words, w-4, 2, fill.Fg(ink.Text.FG), fill.Fg(m.pal.Orange.FG).With(canvas.Bold)) {
		out = append(out, canvas.Fit(append(canvas.Row{lead}, r...), w, fill))
	}
	return out
}

// overlaySearch draws the search over the frame, which goes faint behind
// it, as ctrl+k's bar does.
func (m *Model) overlaySearch(v store.View, frame []canvas.Row) []canvas.Row {
	ink := m.pal.Main
	f := &m.find
	bw := min(m.w-4, 100)
	if bw < 30 {
		return frame
	}
	inner := bw - 4
	top := min(m.h/8, 3)
	edge, fill := m.pal.Orange, ink.Text.Bg(ink.Sel.BG)
	dim := fill.Fg(ink.Dim.FG)

	line := func(r canvas.Row) canvas.Row {
		return append(append(canvas.Row{canvas.T("│ ", edge.Bg(fill.BG))}, canvas.Fit(r, inner, fill)...), canvas.T(" │", edge.Bg(fill.BG)))
	}
	edgeRow := func(l, label, right, r string) canvas.Row {
		row := canvas.Row{canvas.T(l+"─ ", edge), canvas.T(label, ink.Sub.With(canvas.Bold))}
		tail := canvas.Row{canvas.T("─"+r, edge)}
		if right != "" {
			tail = append(canvas.Row{canvas.T(" "+right+" ", ink.Dim)}, tail...)
		}
		return append(append(row, canvas.T(" "+strings.Repeat("─", max(0, bw-row.Width()-tail.Width()-1)), edge)), tail...)
	}
	count := ""
	switch {
	case f.busy:
		count = "searching…"
	case f.sent != "" && f.err == "":
		count = plural(f.total, "result")
	}
	box := []canvas.Row{edgeRow("╭", "⌕ Search messages", count, "╮")}

	q := canvas.Row{canvas.T("❯ ", fill.Fg(m.pal.Orange.FG).With(canvas.Bold))}
	for _, t := range strings.SplitAfter(string(f.query), " ") {
		st := fill.Fg(ink.Bright.FG)
		if modifier(t) {
			st = fill.Fg(m.pal.Blue.FG)
		}
		if t != "" {
			q = append(q, canvas.T(t, st))
		}
	}
	box = append(box, line(append(q, canvas.T("▏", fill.Fg(m.pal.Orange.FG)))))
	if id := cmp.Or(f.here, m.open); id != "" {
		if c := v.Conv(id); c != nil {
			chip := canvas.T("◦ in "+convLabel(v, c), dim)
			if f.here != "" {
				chip = canvas.T("⌕ in "+convLabel(v, c), fill.Fg(m.pal.Orange.FG).With(canvas.Bold))
			}
			box = append(box, line(canvas.Row{canvas.T("  ", fill), chip}))
		}
	}
	box = append(box, line(canvas.Row{canvas.T(strings.Repeat("─", inner), fill.Fg(ink.Faint.FG))}))

	// The results, drawn once each, the selection kept in view.
	if inner != f.drawnW || v.Names() != f.drawnNames {
		f.drawn, f.drawnW, f.drawnNames = nil, inner, v.Names()
	}
	words := terms(f.sent)
	rowsOf := func(i int) []canvas.Row {
		for len(f.drawn) <= i {
			f.drawn = append(f.drawn, nil)
		}
		if f.drawn[i] == nil {
			f.drawn[i] = m.resultRows(v, &f.found[i], words, inner, fill)
		}
		return f.drawn[i]
	}
	listH := max(3, m.h-top-len(box)-3)
	f.at = min(f.at, max(0, len(f.found)-1))
	f.top = min(f.top, f.at)
	for f.top < f.at {
		h := 0
		for i := f.top; i <= f.at; i++ {
			h += len(rowsOf(i))
		}
		if h <= listH {
			break
		}
		f.top++
	}
	var list []canvas.Row
	for i := f.top; i < len(f.found) && len(list) < listH; i++ {
		rs := rowsOf(i)
		if i == f.at {
			rs = m.lift(rs, fill)
		}
		list = append(list, rs...)
	}
	note := ""
	switch {
	case f.err != "":
		list = append(list, canvas.Row{canvas.T("  "+f.err, fill.Fg(m.pal.Red.FG))})
	case f.busy:
		note = "searching…"
	case f.sent == "" && len(f.query) == 0:
		note = "words, and in:#channel from:@person before:2026-10-01 after: is:thread"
	case f.sent != "" && !f.busy && len(f.found) == 0:
		note = "nothing found"
	}
	if note != "" {
		list = append(list, canvas.Row{canvas.T("  "+note, dim)})
	}
	for _, r := range list[:min(len(list), listH)] {
		box = append(box, line(r))
	}
	box = append(box, edgeRow("╰", "↑↓ choose · enter go · tab this conversation · esc close", "", "╯"))

	out := make([]canvas.Row, len(frame))
	for i, r := range frame {
		out[i] = faint(r, ink.Faint.FG)
	}
	left := (m.w - bw) / 2
	for i, r := range box {
		if top+i < len(out)-1 {
			out[top+i] = canvas.Splice(out[top+i], left, r)
		}
	}
	return out
}

// lift is rows as the selection: the hover ground and rush's orange bar.
func (m *Model) lift(rows []canvas.Row, fill canvas.Style) []canvas.Row {
	hover := m.pal.Main.Hover.BG
	out := make([]canvas.Row, len(rows))
	for i, r := range rows {
		nr := make(canvas.Row, len(r))
		for j, s := range r {
			if s.St.BG == fill.BG {
				s.St = s.St.Bg(hover)
			}
			nr[j] = s
		}
		nr[0] = canvas.T("▍ ", fill.Bg(hover).Fg(m.pal.Orange.FG))
		out[i] = nr
	}
	return out
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return strconv.Itoa(n) + " " + what + "s"
}
