package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/fuzzy"
)

// One small list over the screen, as ctrl+k's is, for three jobs:
// browsing the public channels you're not in (b, or # in ctrl+k), picking
// who a new DM or group DM is with (N, or n in the DMs tab) and choosing the
// section to move a conversation to (s). Browsing shows a channel read-only
// before joining; the box where you'd write has a join chip instead.

type pickKind uint8

const (
	pickNone pickKind = iota
	pickBrowse
	pickDM
	pickSection
)

type picker struct {
	kind   pickKind
	query  []rune
	at     int
	items  []pickItem
	ticked []string // new DM: who's been picked
	conv   string   // section: the conversation being moved
}

// pickItem is a row: id is the channel, person or section it stands for.
type pickItem struct {
	id, glyph, label, detail, tail string
	lit                            []int // runes of label the query matched
	score                          int
}

const (
	listCap    = 60 // rows kept for the list
	browseCap  = 25 // pages of channels fetched at most
	browseTTL  = 5 * time.Minute
	dmMostFrom = 8 // Slack's group DMs hold eight others
)

// browse is what the channel browser holds between openings.
type browse struct {
	chans []slack.Conversation // the public channels fetched, yours included
	at    time.Time            // when the fetch began
	gen   int                  // which fetch; an older one's pages are dropped
	pages int
	more  bool // pages are still coming
}

type (
	browsedMsg struct {
		gen   int
		chans []slack.Conversation
		next  string
		err   error
	}
	dmMsg struct {
		users []string
		conv  slack.Conversation
		err   error
	}
	joinedMsg struct {
		id   string
		conv slack.Conversation
		err  error
	}
)

// --- browsing ---

func (m *Model) openBrowse() tea.Cmd {
	m.mg.pk = picker{kind: pickBrowse}
	b := &m.br
	var cmd tea.Cmd
	if b.chans == nil || time.Since(b.at) > browseTTL {
		b.chans, b.at, b.gen, b.pages, b.more = nil, time.Now(), b.gen+1, 0, true
		cmd = m.browseNext(b.gen, "")
	}
	m.st.Read(m.buildPick)
	return cmd
}

func (m *Model) browseNext(gen int, cursor string) tea.Cmd {
	return func() tea.Msg {
		chans, next, err := m.api.Browse(m.ctx, cursor)
		return browsedMsg{gen, chans, next, err}
	}
}

func (m *Model) browsed(msg browsedMsg) tea.Cmd {
	b := &m.br
	if msg.gen != b.gen {
		return nil
	}
	var cmd tea.Cmd
	b.pages++
	b.chans = append(b.chans, msg.chans...)
	b.more = msg.err == nil && msg.next != "" && b.pages < browseCap
	if b.more {
		cmd = m.browseNext(msg.gen, msg.next)
	}
	if msg.err != nil {
		b.at = time.Time{} // fetch again next time
		cmd = m.say("couldn't list channels: "+msg.err.Error(), true)
	}
	if m.mg.pk.kind == pickBrowse {
		m.st.Read(m.buildPick)
	}
	return cmd
}

// buildPick lists what the query finds for the open picker.
func (m *Model) buildPick(v store.View) {
	p := &m.mg.pk
	q := string(p.query)
	was := ""
	if p.at < len(p.items) {
		was = p.items[p.at].id
	}
	p.items = p.items[:0]
	switch p.kind {
	case pickBrowse:
		for _, c := range m.br.chans {
			if c.IsMember || c.IsArchived || (v.Conv(c.ID) != nil && !v.Conv(c.ID).Preview) {
				continue
			}
			score, lit, ok := fuzzy.Match(q, c.Name)
			if !ok {
				continue
			}
			it := pickItem{id: c.ID, glyph: "# ", label: c.Name, detail: firstLine(c.Purpose.Value), lit: lit, score: score}
			if c.NumMembers > 0 {
				it.tail = strconv.Itoa(c.NumMembers) + " ⊙"
			}
			p.items = append(p.items, it)
		}
	case pickDM:
		for _, it := range m.people(v, q, nil) {
			id, ok := strings.CutPrefix(it.code, "<@")
			if id = strings.TrimSuffix(id, ">"); !ok || id == v.Self() {
				continue
			}
			pi := pickItem{id: id, glyph: "● ", label: strings.TrimPrefix(it.label, "@"), tail: it.detail, score: it.score}
			if slices.Contains(p.ticked, id) {
				pi.glyph = "✓ "
			}
			for _, i := range it.lit {
				pi.lit = append(pi.lit, i-1)
			}
			p.items = append(p.items, pi)
		}
	}
	slices.SortStableFunc(p.items, func(a, b pickItem) int {
		return cmp.Or(b.score-a.score, strings.Compare(strings.ToLower(a.label), strings.ToLower(b.label)))
	})
	p.items = p.items[:min(len(p.items), listCap)]
	p.at = max(0, slices.IndexFunc(p.items, func(it pickItem) bool { return it.id == was }))
}

// preview shows channel id read-only, with a join chip where the box is.
func (m *Model) preview(id string) tea.Cmd {
	i := slices.IndexFunc(m.br.chans, func(c slack.Conversation) bool { return c.ID == id })
	if i < 0 {
		return nil
	}
	m.st.Preview(m.br.chans[i])
	cmd := m.visit(id)
	m.setFocus(onMsgs)
	return cmd
}

func (m *Model) previewing(v store.View) bool {
	c := v.Conv(m.open)
	return c != nil && c.Preview
}

func (m *Model) previewHints() [][2]string {
	if m.focus == onCompose {
		return [][2]string{{"j enter", "join"}, {"esc", "messages"}}
	}
	return [][2]string{{"↑↓", "move"}, {"tab", "join chip"}, {"esc", "sidebar"}}
}

func (m *Model) isPreview() (on bool) {
	m.st.Read(func(v store.View) { on = m.previewing(v) })
	return on
}

// previewKey is a key at the join chip.
func (m *Model) previewKey(s string) tea.Cmd {
	switch s {
	case "j", "enter":
		id := m.open
		return func() tea.Msg {
			c, err := m.api.Join(m.ctx, id)
			return joinedMsg{id, c, err}
		}
	case "esc":
		m.setFocus(onMsgs)
	}
	return nil
}

func (m *Model) joined(msg joinedMsg) tea.Cmd {
	if msg.err != nil {
		return m.say("couldn't join: "+msg.err.Error(), true)
	}
	c := msg.conv
	m.st.Read(func(v store.View) {
		if p := v.Conv(msg.id); p != nil { // what the preview knew, if Slack's answer is bare
			c.Name, c.NumMembers = cmp.Or(c.Name, p.Name), cmp.Or(c.NumMembers, p.Members)
			c.Topic.Value = cmp.Or(c.Topic.Value, p.Topic)
		}
	})
	c.ID, c.IsChannel = msg.id, true
	m.st.Put(c)
	if m.open == msg.id {
		m.setFocus(onCompose)
	}
	return m.say("joined #"+c.Name, false)
}

// joinBar stands where the box is while a channel is only being read.
func (m *Model) joinBar(c *store.Conv, w int) []canvas.Row {
	ink := m.pal.Main
	edge, on := ink.Edge, m.focus == onCompose
	if on {
		edge = m.pal.Orange
	}
	edgeRow := func(l, label, r string) canvas.Row {
		row := canvas.Row{canvas.T(l, edge), canvas.T(" "+label+" ", ink.Dim)}
		return canvas.Fit(append(row, canvas.T(strings.Repeat("─", max(0, w-row.Width()-1))+r, edge)), w, ink.Text)
	}
	about := fmt.Sprintf("reading # %s", c.Name)
	if c.Members > 0 {
		about += fmt.Sprintf(" · %d members", c.Members)
	}
	join := m.pal.Chip.Bg(m.pal.Green.FG).Fg(ink.Ground)
	if on {
		join = join.With(canvas.Underline | canvas.Bold)
	}
	body := canvas.Row{canvas.T("│ ", edge), canvas.T(" join ", join), canvas.T("  "+firstLine(c.Topic), ink.Dim)}
	inner := canvas.Fit(body, w-2, ink.Text)
	hint := "tab to the join chip"
	if on {
		hint = "j or enter joins · esc back"
	}
	return []canvas.Row{edgeRow("╭", about, "╮"), append(inner, canvas.T(" │", edge)), edgeRow("╰", hint, "╯")}
}

// --- new DMs ---

func (m *Model) openNewDM() tea.Cmd {
	m.mg.pk = picker{kind: pickDM}
	m.st.Read(m.buildPick)
	return nil
}

// openDM opens the DM with ids: the one held if it's a single person's,
// else Slack's answer to conversations.open.
func (m *Model) openDM(ids []string) tea.Cmd {
	if len(ids) == 1 {
		found := ""
		m.st.Read(func(v store.View) {
			for _, id := range m.sideConvs() {
				if c := v.Conv(id); c != nil && c.Kind == store.IM && c.User == ids[0] {
					found = id
				}
			}
		})
		if found != "" {
			m.setFocus(onCompose)
			return m.visit(found)
		}
	}
	return func() tea.Msg {
		c, err := m.api.OpenDM(m.ctx, ids)
		return dmMsg{ids, c, err}
	}
}

func (m *Model) dmOpened(msg dmMsg) tea.Cmd {
	if msg.err != nil || msg.conv.ID == "" {
		err := "no conversation came back"
		if msg.err != nil {
			err = msg.err.Error()
		}
		return m.say("couldn't open that: "+err, true)
	}
	c := msg.conv
	if len(msg.users) == 1 {
		c.IsIM, c.User = true, msg.users[0]
	} else {
		c.IsMPIM, c.IsGroup, c.IsPrivate = true, true, true
		if c.Name == "" {
			var handles []string
			m.st.Read(func(v store.View) {
				for _, id := range msg.users {
					handles = append(handles, v.Person(id).Handle)
				}
			})
			c.Name = "mpdm-" + strings.Join(handles, "--") + "-1"
		}
	}
	m.st.Put(c)
	m.setFocus(onCompose)
	return m.visit(c.ID)
}

// --- moving ---

func (m *Model) openMove() tea.Cmd {
	id := m.selConv()
	if id == "" {
		return nil
	}
	p := picker{kind: pickSection, conv: id}
	m.st.Read(func(v store.View) {
		c := v.Conv(id)
		if c == nil {
			return
		}
		here := v.SectionOf(id)
		for _, sec := range v.Sections() {
			if sec.Type == "standard" || sec.Type == "stars" {
				p.items = append(p.items, pickItem{id: sec.ID, glyph: "  ", label: sec.Name})
				if sec.ID == here {
					p.at, p.items[len(p.items)-1].glyph = len(p.items)-1, "✓ "
				}
			}
		}
		home := "Channels"
		switch {
		case c.Kind == store.IM && v.Person(c.User).Bot:
			home = "Apps"
		case c.Kind == store.IM || c.Kind == store.MPIM:
			home = "Direct messages"
		}
		p.items = append(p.items, pickItem{glyph: "  ", label: home, detail: "where it goes by its kind"})
		if here == "" {
			p.at, p.items[len(p.items)-1].glyph = len(p.items)-1, "✓ "
		}
	})
	if len(p.items) == 1 {
		return m.say("you've no sections of your own to move it to", false)
	}
	m.mg.pk = p
	return nil
}

// --- keys ---

func (m *Model) pickKey(k tea.KeyPressMsg) tea.Cmd {
	p := &m.mg.pk
	s := k.String()
	if p.kind == pickSection { // no box to type in, so j and k move
		switch s {
		case "j":
			s = "down"
		case "k":
			s = "up"
		}
	}
	rebuild := func() { m.st.Read(m.buildPick) }
	switch s {
	case "esc", "ctrl+c":
		*p = picker{}
	case "up", "ctrl+p":
		if n := len(p.items); n > 0 {
			p.at = (p.at + n - 1) % n
		}
	case "down", "ctrl+n":
		if n := len(p.items); n > 0 {
			p.at = (p.at + 1) % n
		}
	case "enter":
		return m.pickEnter()
	case "tab":
		if p.kind == pickDM && p.at < len(p.items) {
			id := p.items[p.at].id
			if i := slices.Index(p.ticked, id); i >= 0 {
				p.ticked = slices.Delete(p.ticked, i, i+1)
			} else if len(p.ticked) < dmMostFrom {
				p.ticked = append(p.ticked, id)
			}
			p.query = p.query[:0]
			rebuild()
		}
	case "backspace":
		switch {
		case len(p.query) > 0:
			p.query = p.query[:len(p.query)-1]
			rebuild()
		case len(p.ticked) > 0:
			p.ticked = p.ticked[:len(p.ticked)-1]
			rebuild()
		}
	case "ctrl+u", "ctrl+w":
		p.query = p.query[:0]
		rebuild()
	default:
		if k.Text != "" && p.kind != pickSection {
			p.query = append(p.query, []rune(k.Text)...)
			rebuild()
		}
	}
	return nil
}

func (m *Model) pickEnter() tea.Cmd {
	p := m.mg.pk
	switch p.kind {
	case pickBrowse:
		if p.at < len(p.items) {
			m.mg.pk = picker{}
			return m.preview(p.items[p.at].id)
		}
	case pickDM:
		ids := slices.Clone(p.ticked)
		if len(ids) == 0 && p.at < len(p.items) {
			ids = []string{p.items[p.at].id}
		}
		if len(ids) > 0 {
			m.mg.pk = picker{}
			return m.openDM(ids)
		}
	case pickSection:
		if p.at < len(p.items) {
			m.mg.pk = picker{}
			return m.moveTo(p.conv, p.items[p.at].id, "moved")
		}
	}
	return nil
}

// --- drawing ---

var pickTitle = [...]string{pickBrowse: "# Browse channels", pickDM: "✎ New message", pickSection: "▾ Move to section"}

// overlayPick draws the picker over the frame, which goes faint behind it.
func (m *Model) overlayPick(v store.View, frame []canvas.Row) []canvas.Row {
	p := &m.mg.pk
	ink := m.pal.Main
	bw := min(m.w-4, 72)
	if bw < 24 {
		return frame
	}
	inner := bw - 4
	top := min(m.h/8, 4)
	listH := max(1, min(max(len(p.items), 3), min(12, m.h*2/3-4)))
	edge, fill := m.pal.Orange, ink.Text.Bg(ink.Sel.BG)

	line := func(r canvas.Row) canvas.Row {
		return append(append(canvas.Row{canvas.T("│ ", edge.Bg(fill.BG))}, canvas.Fit(r, inner, fill)...), canvas.T(" │", edge.Bg(fill.BG)))
	}
	edgeRow := func(l, label, r string) canvas.Row {
		row := canvas.Row{canvas.T(l+"─ ", edge), canvas.T(label, ink.Sub.With(canvas.Bold))}
		return append(row, canvas.T(" "+strings.Repeat("─", max(0, bw-row.Width()-2))+r, edge))
	}
	title, keys := pickTitle[p.kind], "↑↓ choose · enter preview · esc close"
	switch p.kind {
	case pickDM:
		keys = "↑↓ choose · tab pick several · enter open · esc close"
	case pickSection:
		title, keys = "▾ Move "+m.pickName(v)+" to", "↑↓ choose · enter move · esc close"
	}
	box := []canvas.Row{edgeRow("╭", title, "╮")}
	if p.kind != pickSection {
		q := canvas.Row{canvas.T("❯ ", fill.Fg(m.pal.Orange.FG).With(canvas.Bold))}
		for _, id := range p.ticked {
			q = append(q, canvas.T(v.Person(id).Name+", ", fill.Fg(m.pal.Blue.FG)))
		}
		q = append(q, canvas.T(string(p.query), fill.Fg(ink.Bright.FG)), canvas.T("▏", fill.Fg(m.pal.Orange.FG)))
		box = append(box, line(q), line(canvas.Row{canvas.T(strings.Repeat("─", inner), fill.Fg(ink.Faint.FG))}))
	}
	rows := make([]canvas.Row, 0, len(p.items))
	for i, it := range p.items {
		rows = append(rows, m.pickRow(it, inner, i == p.at, fill))
	}
	if len(rows) == 0 {
		msg := "  nothing matches"
		if p.kind == pickBrowse && m.br.more {
			msg = "  looking…"
		}
		rows = append(rows, canvas.Row{canvas.T(msg, fill.Fg(ink.Dim.FG))})
	}
	from := max(0, p.at-listH+1)
	for _, r := range rows[from:min(len(rows), from+listH)] {
		box = append(box, line(r))
	}
	box = append(box, edgeRow("╰", keys, "╯"))

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

// pickName is the conversation being moved, as the sidebar names it.
func (m *Model) pickName(v store.View) string {
	if c := v.Conv(m.mg.pk.conv); c != nil {
		return convLabel(v, c)
	}
	return "it"
}

func (m *Model) pickRow(it pickItem, w int, sel bool, fill canvas.Style) canvas.Row {
	ink := m.pal.Main
	base := fill
	mark := canvas.T("  ", base)
	if sel {
		base = fill.Bg(ink.Hover.BG)
		mark = canvas.T("▍ ", base.Fg(m.pal.Orange.FG))
	}
	row := canvas.Row{mark, canvas.T(it.glyph, base.Fg(ink.Dim.FG))}
	lit := base.Fg(m.pal.Orange.FG).With(canvas.Bold)
	for i, r := range []rune(it.label) {
		st := base.Fg(ink.Text.FG)
		if slices.Contains(it.lit, i) {
			st = lit
		}
		row = append(row, canvas.T(string(r), st)) // ponytail: a seg a rune; labels are short
	}
	var tail canvas.Row
	if it.tail != "" {
		tail = canvas.Row{canvas.T(it.tail+" ", base.Fg(ink.Dim.FG))}
	}
	if it.detail != "" {
		room := max(0, w-row.Width()-tail.Width()-3)
		row = append(row, canvas.T("  ", base))
		row = append(row, canvas.Cut(canvas.Row{canvas.T(it.detail, base.Fg(ink.Dim.FG))}, room)...)
	}
	return rightAlign(row, tail, w, base)
}
