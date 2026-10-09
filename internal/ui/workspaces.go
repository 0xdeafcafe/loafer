package ui

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/frame"
	"github.com/0xdeafcafe/photon/fuzzy"
	"github.com/0xdeafcafe/photon/theme"
)

// Several workspaces at once: a Model each, every one booted and live
// over its own websocket, and one of them shown. What a Model's commands
// return comes back to that Model, tagged on the way; the terminal's keys
// go to the one shown, and its size, colours and focus to them all. So
// switching is only changing which is shown, and each keeps its place:
// what's open, the drafts, the scroll. One behind fetches no messages and
// marks nothing read; it keeps its socket, its unreads and its
// notifications. With more than one, a rail down the left shows them.

// Multi is the bubbletea model for one or more workspaces.
type Multi struct {
	ws   []*Model
	at   int // the one shown
	w    int
	seen []uint64    // the store version each rail entry was made from
	ents []railEntry // the rail, a workspace each
	rail railCache
}

// wsSlot is a Model's place in a Multi; nil when it's the only workspace.
type wsSlot struct {
	x *Multi
	i int
}

type (
	wsMsg struct { // what workspace i's command came back with
		i   int
		msg tea.Msg
	}
	switchMsg struct{ to *Model } // show this workspace
)

// railW is the rail's width: the marker, two cells of initials, a gap.
const railW = 4

// NewMulti shows ms, the first first.
func NewMulti(ms ...*Model) *Multi {
	x := &Multi{ws: ms, seen: make([]uint64, len(ms)), ents: make([]railEntry, len(ms))}
	if len(ms) > 1 {
		for i, m := range ms {
			m.ws = &wsSlot{x, i}
		}
	}
	return x
}

func (x *Multi) Init() tea.Cmd {
	cmds := make([]tea.Cmd, len(x.ws))
	for i, m := range x.ws {
		cmds[i] = tag(i, m.Init())
	}
	return tea.Batch(cmds...)
}

func (x *Multi) View() tea.View { return x.ws[x.at].View() }

// SignedOut says whether it quit because Slack signed every workspace out.
func (x *Multi) SignedOut() bool {
	for _, m := range x.ws {
		if !m.SignedOut() {
			return false
		}
	}
	return true
}

// tag has what cmd returns come back as workspace i's.
func tag(i int, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg { return wsMsg{i, cmd()} }
}

var teaPkg = reflect.TypeFor[tea.QuitMsg]().PkgPath()

func (x *Multi) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case wsMsg:
		return x, x.route(msg)
	case tea.WindowSizeMsg:
		x.w = msg.Width
		if len(x.ws) > 1 {
			msg.Width -= railW
		}
		return x, x.all(msg)
	case uv.PixelSizeEvent:
		if len(x.ws) > 1 && x.w > 0 {
			msg.Width = msg.Width * (x.w - railW) / x.w // the panes' share of it
		}
		return x, x.all(msg)
	case tea.BackgroundColorMsg, tea.FocusMsg, tea.BlurMsg, uv.CellSizeEvent:
		return x, x.all(msg)
	case tea.KeyPressMsg:
		if msg.String() == "alt+w" && len(x.ws) > 1 {
			return x, x.show(x.ws[(x.at+1)%len(x.ws)])
		}
	}
	return x, x.to(x.at, msg)
}

func (x *Multi) all(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, len(x.ws))
	for i := range x.ws {
		cmds[i] = x.to(i, msg)
	}
	return tea.Batch(cmds...)
}

// route takes what workspace w.i's command returned.
func (x *Multi) route(w wsMsg) tea.Cmd {
	switch msg := w.msg.(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		cmds := make([]tea.Cmd, len(msg))
		for j, c := range msg {
			cmds[j] = tag(w.i, c)
		}
		return tea.Batch(cmds...)
	case switchMsg:
		return x.show(msg.to)
	case picsMsg:
		// The pictures are the first workspace's (picture.go); a landing
		// is drawn again by every one.
		for j, m := range x.ws {
			if j != w.i {
				m.drawn.Clear()
				m.claude.redraw()
			}
		}
	}
	if reflect.TypeOf(w.msg).PkgPath() == teaPkg {
		// Quit, the clipboard, a raw escape: bubbletea's own to do.
		return func() tea.Msg { return w.msg }
	}
	if w.i == x.at {
		return x.to(w.i, w.msg)
	}
	cmd := x.to(w.i, w.msg)
	if w.i < len(x.rail.ents) && x.entry(w.i) == x.rail.ents[w.i] {
		x.ws[x.at].gate.Keep() // its rail entry is as drawn, so nothing on screen changed
	}
	return cmd
}

// to hands msg to workspace i. One that Slack has just signed out
// quits only when it was the last; else the rest carry on, and its rail
// says so.
func (x *Multi) to(i int, msg tea.Msg) tea.Cmd {
	m := x.ws[i]
	was := m.SignedOut()
	_, cmd := m.Update(msg)
	if was || !m.SignedOut() {
		return tag(i, cmd)
	}
	if x.SignedOut() {
		return tea.Quit // run signs in again and reopens
	}
	shown := x.ws[x.at]
	return tag(x.at, shown.say("slack signed you out of "+m.teamName()+" · run loafer login", true))
}

// show brings workspace to to the front.
func (x *Multi) show(to *Model) tea.Cmd {
	j := slices.Index(x.ws, to)
	if j < 0 || j == x.at {
		return nil
	}
	from := x.ws[x.at]
	x.at = j
	from.watch()           // behind now, so its open conversation notifies again
	to.gate = frame.Gate{} // what it drew last is from before it went behind
	return tag(j, to.shown())
}

// hidden is whether the model is a workspace behind the one shown.
func (m *Model) hidden() bool { return m.ws != nil && m.ws.x.ws[m.ws.x.at] != m }

// shown is the model coming to the front: what's open is in front of you
// again, and one that booted behind opens its conversation now.
func (m *Model) shown() tea.Cmd {
	m.watch()
	if m.SignedOut() {
		return m.say("slack signed you out of "+m.teamName()+" · run loafer login", true)
	}
	if m.open == "" {
		m.st.Read(m.buildSide)
		return m.openSelected()
	}
	if m.scroll == 0 {
		return m.markRead() // what came in behind is read once it's seen
	}
	return nil
}

// teamName is the workspace's name, from boot or the cache, else from
// the sign-in.
func (m *Model) teamName() (name string) {
	m.st.Read(func(v store.View) { name = v.Team().Name })
	if name == "" && m.api != nil {
		name = m.api.Team()
	}
	return name
}

// titled is a notification's title, saying which workspace when there's
// more than one.
func (m *Model) titled(title string) string {
	if m.ws == nil {
		return title
	}
	return m.teamName() + " · " + title
}

// --- the rail ---

// railEntry is what the rail shows of a workspace.
type railEntry struct {
	initials string
	colour   theme.RGB
	unread   bool
	mentions int
	out      bool // signed out
}

// entry is workspace i's rail entry, made again only when its store or
// sign-in has changed.
func (x *Multi) entry(i int) railEntry {
	m := x.ws[i]
	if v := m.st.Version(); v != x.seen[i] || x.ents[i].out != m.SignedOut() || x.ents[i].initials == "" {
		e := railEntry{out: m.SignedOut()}
		m.st.Read(func(v store.View) {
			e.colour = teamColour(v.Team())
			for _, sec := range v.Sidebar() {
				for _, id := range sec.Convs {
					if c := v.Conv(id); c != nil {
						e.mentions += c.Mentions
						e.unread = e.unread || c.Unread
					}
				}
			}
		})
		e.initials = teamInitials(m.teamName())
		x.seen[i], x.ents[i] = v, e
	}
	return x.ents[i]
}

// teamInitials are a name's first two words' first letters, skipping
// words like "&", or a one-word name's first two letters: "Crumb & Co"
// is CC, "LangWatch" La.
func teamInitials(name string) string {
	var words [][]rune
	for f := range strings.FieldsSeq(name) {
		if r := []rune(f); unicode.IsLetter(r[0]) || unicode.IsDigit(r[0]) {
			words = append(words, r)
		}
	}
	switch {
	case len(words) == 0:
		return "··"
	case len(words) == 1 && len(words[0]) > 1:
		return string(unicode.ToUpper(words[0][0])) + string(unicode.ToLower(words[0][1]))
	case len(words) == 1:
		return string(unicode.ToUpper(words[0][0])) + " "
	}
	return string(unicode.ToUpper(words[0][0])) + string(unicode.ToUpper(words[1][0]))
}

type railCache struct {
	h, at int
	pal   theme.RGB // the Side ground they were drawn on
	ents  []railEntry
	rows  []canvas.Row
}

// railed is the frame with the rail down its left, when there's more than
// one workspace.
func (m *Model) railed(rows []canvas.Row) []canvas.Row {
	if m.ws == nil || len(rows) != m.h {
		return rows
	}
	return canvas.Join(m.ws.x.railRows(m.pal, m.h), rows)
}

// railRows is the rail, h rows: each workspace's initials on its colour,
// the shown one marked, and under them its mentions, a dot for unread, or
// ✗ once it's signed out. Drawn again only when one of those changes.
func (x *Multi) railRows(p Palette, h int) []canvas.Row {
	for i := range x.ws {
		x.entry(i)
	}
	c := &x.rail
	if c.h == h && c.at == x.at && c.pal == p.Side.Ground && slices.Equal(c.ents, x.ents) {
		return c.rows
	}
	ground := canvas.Style{}.Bg(theme.Mix(p.Side.Ground, rgb(0, 0, 0), 0.3))
	ink := ground.Fg(p.Side.Text.FG)
	rows := make([]canvas.Row, 0, h)
	for i, e := range x.ents {
		if len(rows)+2 > h {
			break
		}
		mark := canvas.T(" ", ground)
		if i == x.at {
			mark = canvas.T("▍", ground.Fg(p.Orange.FG))
		}
		chip := canvas.Style{}.Bg(e.colour).Fg(railInk(e.colour)).With(canvas.Bold)
		if e.out {
			chip = ground.Fg(p.Side.Faint.FG)
		}
		under := canvas.T("    ", ground)
		switch {
		case e.out:
			under = canvas.T(" ✗  ", ground.Fg(p.Red.FG).With(canvas.Bold))
		case e.mentions > 0:
			n := strconv.Itoa(e.mentions)
			if e.mentions > 9 {
				n = "+"
			}
			under = canvas.T(" @"+n+" ", ground.Fg(p.SideYellow.FG).With(canvas.Bold))
		case e.unread:
			under = canvas.T(" •  ", ground.Fg(p.Side.Bright.FG))
		}
		rows = append(rows, canvas.Row{mark, canvas.T(e.initials, chip), canvas.T(" ", ink)}, canvas.Row{under})
		if len(rows) < h {
			rows = append(rows, canvas.Row{canvas.T("    ", ground)})
		}
	}
	for len(rows) < h {
		rows = append(rows, canvas.Row{canvas.T("    ", ground)})
	}
	*c = railCache{h, x.at, p.Side.Ground, slices.Clone(x.ents), rows}
	return rows
}

// --- ctrl+k ---

// jumpWorkspaces are the other workspaces for ctrl+k, those matching q.
func (m *Model) jumpWorkspaces(q string) []jumpItem {
	if m.ws == nil {
		return nil
	}
	var out []jumpItem
	for _, o := range m.ws.x.ws {
		if o == m {
			continue
		}
		it := jumpItem{ws: o, section: "Workspaces"}
		if q != "" {
			score, lit, ok := fuzzy.Match(q, o.teamName())
			if !ok {
				continue
			}
			it.lit, it.score = lit, score
		}
		out = append(out, it)
	}
	return out
}

// jumpWsRow is a workspace in ctrl+k: its initials, its name and its
// mentions, or that it's signed out.
func (m *Model) jumpWsRow(it jumpItem, w int, sel bool, fill canvas.Style) canvas.Row {
	ink := m.pal.Main
	x := m.ws.x
	e := x.entry(slices.Index(x.ws, it.ws))
	base := fill
	mark := canvas.T("  ", base)
	if sel {
		base = fill.Bg(ink.Hover.BG)
		mark = canvas.T("▍ ", base.Fg(m.pal.Orange.FG))
	}
	row := canvas.Row{mark, canvas.T(e.initials, canvas.Style{}.Bg(e.colour).Fg(railInk(e.colour)).With(canvas.Bold)), canvas.T(" ", base)}
	name, lit := base.Fg(ink.Text.FG), base.Fg(m.pal.Orange.FG).With(canvas.Bold)
	for i, r := range []rune(it.ws.teamName()) {
		st := name
		if slices.Contains(it.lit, i) {
			st = lit
		}
		row = append(row, canvas.T(string(r), st))
	}
	var tail canvas.Row
	switch {
	case e.out:
		tail = canvas.Row{canvas.T("signed out · run loafer login ", base.Fg(m.pal.Red.FG))}
	case e.mentions > 0:
		tail = canvas.Row{canvas.T("@"+strconv.Itoa(e.mentions)+" ", base.Fg(m.pal.Yellow.FG).With(canvas.Bold))}
	case e.unread:
		tail = canvas.Row{canvas.T("unread ", base.Fg(ink.Dim.FG))}
	}
	return rightAlign(row, canvas.Cut(tail, max(0, w-row.Width()-2)), w, base)
}
