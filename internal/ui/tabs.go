package ui

import (
	"cmp"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/mrkdwn"
	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/cellw"
)

// The header's tabs (docs/ui.md): Home is the sidebar and a conversation;
// DMs, Activity and Later put their list where the sidebar goes, with the
// conversation enter opens beside it; Claude waits on rush. alt+1 to
// alt+5 go between them. Each list is fetched the first time its tab
// opens, and the store keeps it live after.

type tabID uint8

const (
	tabHome tabID = iota
	tabDMs
	tabActivity
	tabLater
	tabClaude
)

var tabNames = [...]string{"Home", "DMs", "Activity", "Later", "Claude"}

// list is the store's list a tab shows; tabs without one have none.
func (t tabID) list() (store.List, bool) {
	return store.List(t - tabDMs), t >= tabDMs && t <= tabLater
}

type tabState struct {
	on      tabID
	at, top [tabClaude + 1]int // each tab's cursor and first item shown
	items   []tabItem          // the list on show
	built   tabID              // the tab items were built for
	seen    uint64             // the store version they were built from
	w       int                // the width they're drawn at
	busy    [3]bool            // a fetch is out, by store.List

	wantConv, want string // the message enter is on its way to
}

// tabItem is a row of a tab's list, with what it needs drawn and acted on
// copied out of the store.
type tabItem struct {
	head  string // a day's rule; nothing else is set
	key   string // which it is, to keep the cursor on it through changes
	conv  string
	act   store.Activity
	saved store.Saved
	rows  []canvas.Row // as drawn, unselected; nil until drawn
}

// tabMsg is a tab's fetch, or a change to Later, coming back.
type tabMsg struct {
	l     store.List
	fetch bool
	what  string // what was being done, for the error
	err   error
}

// setTab shows tab t, fetching its list if it's never come.
func (m *Model) setTab(t tabID) tea.Cmd {
	if t != m.tabs.on {
		m.tabs.on = t
		m.setFocus(onSide)
	}
	l, ok := t.list()
	if !ok {
		return nil
	}
	loaded := false
	m.st.Read(func(v store.View) { loaded = v.Loaded(l) })
	if loaded {
		return nil
	}
	return m.fetch(l)
}

func (m *Model) fetch(l store.List) tea.Cmd {
	if m.tabs.busy[l] {
		return nil
	}
	m.tabs.busy[l] = true
	what := "load " + strings.ToLower(tabNames[tabDMs+tabID(l)])
	return func() tea.Msg { return tabMsg{l: l, fetch: true, what: what, err: m.st.Fetch(m.ctx, m.api, l)} }
}

// fetchTabs fetches again the lists the websocket says have changed.
func (m *Model) fetchTabs() tea.Cmd {
	var cmds []tea.Cmd
	m.st.Read(func(v store.View) {
		for l := store.ActivityList; l <= store.LaterList; l++ {
			if v.Stale(l) {
				cmds = append(cmds, m.fetch(l))
			}
		}
	})
	return tea.Batch(cmds...)
}

func (m *Model) tabDone(msg tabMsg) tea.Cmd {
	var cmd tea.Cmd
	if msg.fetch {
		m.tabs.busy[msg.l] = false
	}
	if msg.err != nil {
		cmd = m.say("couldn't "+msg.what+": "+msg.err.Error(), true)
		if !msg.fetch {
			cmd = tea.Batch(cmd, m.fetch(msg.l)) // put back what Slack has
		}
	}
	return tea.Batch(cmd, m.fetchTabs())
}

// --- keys ---

func (m *Model) tabKey(s string) tea.Cmd {
	switch s {
	case "up", "k":
		m.moveTab(-1)
	case "down", "j":
		m.moveTab(1)
	case "pgup":
		m.moveTab(-10)
	case "pgdown":
		m.moveTab(10)
	case "home", "g":
		m.tabs.at[m.tabs.on] = 0
		m.moveTab(0)
	case "end", "G":
		m.tabs.at[m.tabs.on] = len(m.tabs.items)
		m.moveTab(0)
	case "/":
		m.openJump()
	case "enter", "l", "right":
		return m.enterTab()
	case "d", "x":
		if m.tabs.on == tabLater {
			return m.finish(s == "d")
		}
	}
	return nil
}

// moveTab moves the cursor d items, onto the nearest that isn't a heading.
func (m *Model) moveTab(d int) {
	items, at := m.tabs.items, &m.tabs.at[m.tabs.on]
	if len(items) == 0 {
		return
	}
	i := min(max(*at+d, 0), len(items)-1)
	dir := 1
	if d < 0 {
		dir = -1
	}
	for _, dir := range []int{dir, -dir} {
		for j := i; j >= 0 && j < len(items); j += dir {
			if items[j].head == "" {
				*at = j
				return
			}
		}
	}
}

func (m *Model) tabItem() (tabItem, bool) {
	if at := m.tabs.at[m.tabs.on]; at < len(m.tabs.items) && m.tabs.items[at].head == "" {
		return m.tabs.items[at], true
	}
	return tabItem{}, false
}

func (m *Model) enterTab() tea.Cmd {
	it, ok := m.tabItem()
	if !ok {
		return nil
	}
	switch m.tabs.on {
	case tabDMs:
		m.setFocus(onCompose)
		return m.visit(it.conv)
	case tabActivity:
		a := it.act
		var mark tea.Cmd
		if a.Unread {
			m.st.ReadActivity(a.Key)
			mark = func() tea.Msg {
				if err := m.api.MarkActivityRead(m.ctx, a.Key, a.FeedTS, a.Type, a.Conv, a.TS); err != nil {
					slog.Warn("activity mark", "err", err)
				}
				return nil
			}
		}
		// A reply goes to its thread's parent, in the channel.
		return tea.Batch(mark, m.goTo(a.Conv, cmp.Or(a.ThreadTS, a.TS)))
	case tabLater:
		x := it.saved
		if x.Type != "message" {
			return m.say("that one only opens in Slack", false)
		}
		return m.goTo(x.Conv, cmp.Or(x.Msg.ThreadTS, x.TS))
	}
	return nil
}

// goTo opens conv with the cursor on its message at ts, once it's there.
func (m *Model) goTo(conv, ts string) tea.Cmd {
	known := false
	m.st.Read(func(v store.View) { known = v.Conv(conv) != nil })
	if !known {
		return m.say("that's somewhere you aren't", false)
	}
	m.tabs.wantConv, m.tabs.want = conv, ts
	cmd := m.visit(conv)
	m.setFocus(onMsgs)
	if cmd == nil { // it's open already
		return m.seek()
	}
	return cmd
}

// seek puts the cursor on the message goTo was after, when its
// conversation's open. shortcut: one older than the window held lands on
// the oldest held (which fetches the page before); fetch around it if
// that's not near enough.
func (m *Model) seek() tea.Cmd {
	if m.tabs.wantConv == "" || m.tabs.wantConv != m.open {
		return nil
	}
	ts := m.tabs.want
	m.tabs.wantConv, m.tabs.want = "", ""
	return m.pick(func(ms []slack.Message, _ int) int { return min(msgIndex(ms, ts), len(ms)-1) })
}

// finish takes the Later item under the cursor off the list: done marks
// it complete, else it's removed.
func (m *Model) finish(done bool) tea.Cmd {
	it, ok := m.tabItem()
	if !ok {
		return nil
	}
	x := it.saved
	m.st.Unsave(x.Conv, x.TS)
	if done {
		return tea.Batch(m.say("done", false), func() tea.Msg {
			return tabMsg{l: store.LaterList, what: "mark that done", err: m.api.CompleteSaved(m.ctx, x.Type, x.Conv, x.TS)}
		})
	}
	return tea.Batch(m.say("taken off later", false), func() tea.Msg {
		return tabMsg{l: store.LaterList, what: "take that off later", err: m.api.Unsave(m.ctx, x.Type, x.Conv, x.TS)}
	})
}

func (m *Model) tabHints() [][2]string {
	switch m.tabs.on {
	case tabDMs:
		return [][2]string{{"↑↓", "move"}, {"enter", "open"}, {"alt+1-5", "tabs"}, {"tab", "messages"}}
	case tabActivity:
		return [][2]string{{"↑↓", "move"}, {"enter", "go to it"}, {"alt+1-5", "tabs"}, {"tab", "messages"}}
	case tabLater:
		return [][2]string{{"↑↓", "move"}, {"enter", "go to it"}, {"d", "done"}, {"x", "remove"}, {"alt+1-5", "tabs"}}
	}
	return [][2]string{{"alt+1", "home"}}
}

// --- drawing ---

// tabRow is the header's second row: the tabs, with their badges, yellow
// where they're things that want you.
func (m *Model) tabRow(v store.View) canvas.Row {
	ink := m.pal.Side
	row := canvas.Row{canvas.T(" ", ink.Text)}
	for t, name := range tabNames {
		st := ink.Dim
		if tabID(t) == m.tabs.on {
			st = ink.Sel.With(canvas.Bold)
		}
		row = append(row, canvas.T(" "+name+" ", st))
		if l, ok := tabID(t).list(); ok {
			if n := v.Badge(l); n > 0 {
				b := m.pal.SideYellow
				if l == store.LaterList {
					b = ink.Sub
				}
				row = append(row, canvas.T("·"+strconv.Itoa(n)+" ", b))
			}
		}
	}
	return canvas.Fit(row, m.w, ink.Text)
}

// left is the pane where the sidebar goes: the sidebar on Home, else the
// tab's list, a little wider.
func (m *Model) left(v store.View, h int) (int, []canvas.Row) {
	if m.tabs.on == tabHome {
		w := min(34, max(24, m.w/4))
		return w, m.sidebar(v, w, h)
	}
	w := min(56, max(28, m.w*2/5))
	return w, m.tabList(v, w, h)
}

func (m *Model) buildTab(v store.View) {
	t := &m.tabs
	key := ""
	if it, ok := m.tabItem(); ok && t.built == t.on {
		key = it.key
	}
	t.items = t.items[:0]
	switch t.on {
	case tabDMs:
		for _, id := range v.DMs() {
			t.items = append(t.items, tabItem{key: id, conv: id})
		}
	case tabActivity:
		now, head := time.Now(), ""
		for _, a := range v.Activity() {
			if d := day(tsTime(a.FeedTS), now); d != head {
				head = d
				t.items = append(t.items, tabItem{head: d})
			}
			t.items = append(t.items, tabItem{key: a.Key, act: a})
		}
	case tabLater:
		for _, x := range v.Saved() {
			t.items = append(t.items, tabItem{key: x.Conv + "/" + x.TS, saved: x})
		}
	}
	at := &t.at[t.on]
	for i, it := range t.items {
		if key != "" && it.key == key {
			*at = i
		}
	}
	m.moveTab(0)
}

// tabList draws the tab's list w wide and h tall: its title, then the
// items from the first shown, keeping the cursor's in view.
func (m *Model) tabList(v store.View, w, h int) []canvas.Row {
	ink := m.pal.Main
	t := &m.tabs
	if t.seen != m.st.Version() || t.built != t.on || t.w != w {
		m.buildTab(v)
		t.seen, t.built, t.w = m.st.Version(), t.on, w
	}
	out := make([]canvas.Row, 0, h)
	title := strings.ToUpper(tabNames[t.on])
	if t.on == tabDMs {
		title = "DIRECT MESSAGES"
	}
	out = append(out, canvas.Fit(canvas.Row{canvas.T(" "+title, ink.Sub.With(canvas.Bold))}, w, ink.Text))
	if len(t.items) == 0 && t.on != tabClaude {
		note := "  nothing here"
		if l, _ := t.on.list(); t.busy[l] {
			note = "  loading…"
		}
		out = append(out, canvas.Fit(canvas.Row{canvas.T(note, ink.Dim)}, w, ink.Text))
	}

	now := time.Now()
	draw := func(i int) []canvas.Row {
		it := &t.items[i]
		if it.rows == nil {
			it.rows = m.tabRows(v, it, w, now)
		}
		return it.rows
	}
	at, top := t.at[t.on], &t.top[t.on]
	if at < *top {
		*top = at
	}
	if at > 0 && at == *top && t.items[at-1].head != "" {
		*top = at - 1 // the day's heading comes too
	}
	for n := 0; *top < at; *top++ {
		n = 0
		for i := *top; i <= at && i < len(t.items); i++ {
			n += len(draw(i))
		}
		if n <= h-len(out) {
			break
		}
	}
	for i := *top; i < len(t.items) && len(out) < h; i++ {
		rows := draw(i)
		if i == at {
			if m.focus == onSide {
				rows = m.picked(rows)
			} else {
				rail := make([]canvas.Row, len(rows))
				for j, r := range rows {
					rail[j] = append(canvas.Row{canvas.T("▍", m.pal.Orange)}, r[1:]...)
				}
				rows = rail
			}
		}
		out = append(out, rows[:min(len(rows), h-len(out))]...)
	}
	for len(out) < h {
		out = append(out, canvas.Fit(nil, w, ink.Text))
	}
	return out
}

// tabRows draws one item w wide. Every row's first cell is the cursor's.
// ponytail: times are as of drawing, and items are drawn again only when
// the store changes, so "due today" turns "overdue" on the next change.
func (m *Model) tabRows(v store.View, it *tabItem, w int, now time.Time) []canvas.Row {
	ink := m.pal.Main
	if it.head != "" {
		head := canvas.Row{canvas.T(" "+it.head+" ", ink.Sub.With(canvas.Bold))}
		head = append(head, canvas.T(strings.Repeat("─", max(0, w-head.Width())), ink.Faint))
		return []canvas.Row{canvas.Fit(head, w, ink.Text)}
	}
	switch m.tabs.on {
	case tabDMs:
		return m.dmRows(v, it.conv, w, now)
	case tabActivity:
		return m.activityRows(v, &it.act, w, now)
	}
	return m.laterRows(v, &it.saved, w, now)
}

// dmRows is a DM as rush's two-row agent layout: who and when, then the
// latest message under a ╰.
func (m *Model) dmRows(v store.View, conv string, w int, now time.Time) []canvas.Row {
	ink := m.pal.Main
	c := v.Conv(conv)
	if c == nil {
		return []canvas.Row{canvas.Fit(nil, w, ink.Text)}
	}
	glyph := "● "
	switch {
	case c.Kind == store.MPIM:
		glyph = "⁂ "
	case v.Person(c.User).Bot:
		glyph = "◇ "
	}
	name := ink.Text
	if c.Unread {
		name = ink.Bright.With(canvas.Bold)
	}
	var right canvas.Row
	if c.Latest != "" {
		right = canvas.Row{canvas.T(" "+when(tsTime(c.Latest), now)+" ", ink.Dim)}
	}
	top := rightAlign(canvas.Row{canvas.T(" ", ink.Text), canvas.T(" "+glyph, ink.Dim), canvas.T(v.Title(c), name)}, right, w, ink.Text)
	preview := ""
	if msg, ok := v.LastMsg(conv); ok {
		preview = firstLine(plainText(v, msg.Text))
		if msg.User == v.Self() {
			preview = "you: " + preview
		}
	}
	return []canvas.Row{top, summary(&m.pal, preview, w)}
}

// summary is the ╰ line under a two-row item.
func summary(p *Palette, text string, w int) canvas.Row {
	return canvas.Fit(canvas.Row{canvas.T(" ", p.Main.Text), canvas.T("   ╰ ", p.Main.Faint), canvas.T(text, p.Main.Dim)}, w, p.Main.Text)
}

// activityRows is an activity on one row: what sort, who, where, the
// message with its mentions coloured, and when. Unread ones are bold.
func (m *Model) activityRows(v store.View, a *store.Activity, w int, now time.Time) []canvas.Row {
	p := &m.pal
	ink := p.Main
	glyph, gst := "· ", ink.Dim
	switch a.Type {
	case "at_user", "at_user_group", "at_channel", "at_everyone", "keyword":
		glyph, gst = "@ ", p.Yellow
	case "thread_v2":
		glyph, gst = "↩ ", ink.Sub
	case "message_reaction":
		glyph, gst = "☺ ", ink.Sub
	case "dm":
		glyph, gst = "● ", ink.Sub
	case "bot_dm_bundle":
		glyph = "◇ "
	}
	who := "…"
	switch {
	case a.Reactor != "":
		who = v.Person(a.Reactor).Name
	case a.Msg.TS != "":
		who, _ = author(v, &a.Msg)
	}
	base := ink.Text
	if a.Unread {
		base = ink.Bright.With(canvas.Bold)
	}
	row := canvas.Row{canvas.T(" ", ink.Text), canvas.T(" "+glyph, gst)}
	row = append(row, canvas.Fit(canvas.Row{canvas.T(who, base)}, 14, ink.Text)...)
	row = append(row, canvas.T("  ", ink.Text))
	if a.Reaction != "" {
		e, _ := emoji(a.Reaction)
		row = append(row, canvas.T(e+" ", ink.Text))
	}
	if c := v.Conv(a.Conv); c != nil && c.Kind <= store.Private {
		row = append(row, canvas.T(convLabel(v, c)+"  ", ink.Dim))
	}
	if lines := mrkdwn.Parse(a.Msg.Text); len(lines) > 0 {
		row = append(row, styled(p, v, lines[0].Spans, base)...)
	}
	right := canvas.Row{canvas.T(" "+when(tsTime(a.FeedTS), now)+" ", ink.Dim)}
	return []canvas.Row{rightAlign(row, right, w, ink.Text)}
}

// laterRows is a saved item: where and whose, when it's due, then the
// message under a ╰. Overdue is red.
func (m *Model) laterRows(v store.View, x *store.Saved, w int, now time.Time) []canvas.Row {
	p := &m.pal
	ink := p.Main
	row := canvas.Row{canvas.T(" ", ink.Text), canvas.T(" ◆ ", p.Orange)}
	c := v.Conv(x.Conv)
	if c != nil {
		row = append(row, canvas.T(convLabel(v, c)+"  ", ink.Dim))
	}
	if x.Msg.TS != "" && c != nil && c.Kind <= store.Private {
		who, _ := author(v, &x.Msg)
		row = append(row, canvas.T(who, ink.Text))
	}
	var right canvas.Row
	if x.Due > 0 {
		due := time.Unix(x.Due, 0)
		switch {
		case due.Before(now):
			right = canvas.Row{canvas.T(" overdue "+ago(now.Sub(due))+" ", p.Red.With(canvas.Bold))}
		case sameDay(due, now):
			right = canvas.Row{canvas.T(" due today "+due.Format("15:04")+" ", p.Yellow)}
		default:
			right = canvas.Row{canvas.T(" due "+strings.ToLower(due.Format("2 Jan"))+" ", ink.Dim)}
		}
	}
	text := firstLine(plainText(v, x.Msg.Text))
	if x.Type != "message" {
		text = "a " + x.Type + ", in Slack"
	}
	return []canvas.Row{rightAlign(row, right, w, ink.Text), summary(p, text, w)}
}

// when is a list's short time: the clock today, the day this past week,
// else the date.
func when(t, now time.Time) string {
	switch {
	case sameDay(t, now):
		return t.Format("15:04")
	case t.Before(now) && now.Sub(t) < 6*24*time.Hour:
		return strings.ToLower(t.Format("Monday"))
	}
	return strings.ToLower(t.Format("2 Jan"))
}

// ago is a while, short: 3m, 5h, 2d.
func ago(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// claude is the Claude tab until it has rush to talk to.
func (m *Model) claude(w, h int) []canvas.Row {
	ink := m.pal.Main
	rows := make([]canvas.Row, h)
	for i := range rows {
		rows[i] = canvas.Fit(nil, w, ink.Text)
	}
	if msg := "Claude needs rush"; h > 2 {
		rows[h/2] = canvas.Fit(canvas.Row{canvas.T(strings.Repeat(" ", max(0, (w-cellw.String(msg))/2))+msg, ink.Dim)}, w, ink.Text)
	}
	return rows
}
