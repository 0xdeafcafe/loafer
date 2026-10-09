package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/cellw"
)

// The profile card (docs/ui.md, Profile card): `p` on a message, or in a
// DM, shows who wrote it over the screen. It reads the store each frame
// it's open, so what users.info brings, a status change or presence all
// land in it as they arrive. Nothing here is cached; one card is cheap.

// profileCard is the card on show.
type profileCard struct {
	on  bool
	id  string
	tz  string         // the zone loc was loaded for
	loc *time.Location // nil when the person has none, or it won't load
}

type dmOpenedMsg struct {
	conv string
	err  error
}

const (
	cardAvatarW = 8
	cardAvatarH = 4
)

// profile opens the card for the author of the message under the cursor,
// else for the other person in a DM.
func (m *Model) profile() tea.Cmd {
	id := ""
	if msg, ok := m.selected(); ok {
		id = msg.User
	} else if m.sel == "" {
		m.st.Read(func(v store.View) {
			if c := v.Conv(m.open); c != nil && c.Kind == store.IM {
				id = c.User
			}
		})
	}
	if id == "" {
		return m.say("pick a message from a person first (↑)", false)
	}
	m.ppl.card = profileCard{on: true, id: id}
	if m.api == nil {
		return nil
	}
	m.api.Watch([]string{id})
	return func() tea.Msg {
		_ = m.st.PersonInfo(m.ctx, m.api, id) // the card reads the store; a failure leaves it as it was
		return nil
	}
}

// cardKey takes the card's keys.
func (m *Model) cardKey(k tea.KeyPressMsg) tea.Cmd {
	c := &m.ppl.card
	switch k.String() {
	case "esc", "q", "p":
		c.on = false
	case "enter", "m":
		c.on = false
		return m.messageUser(c.id)
	case "y", "c":
		c.on = false
		var handle string
		m.st.Read(func(v store.View) { handle = v.Person(c.id).Handle })
		if handle == "" {
			return m.say("no handle to copy", true)
		}
		return tea.Batch(tea.SetClipboard("@"+handle), m.say("copied @"+handle, false))
	}
	return nil
}

// messageUser opens your DM with id, asking Slack for it if there isn't one.
func (m *Model) messageUser(id string) tea.Cmd {
	if m.api == nil {
		return nil
	}
	return func() tea.Msg {
		conv, err := m.st.DM(m.ctx, m.api, id)
		return dmOpenedMsg{conv, err}
	}
}

// onProfile takes the answer to messageUser. It says whether msg was one.
func (m *Model) onProfile(msg tea.Msg) (tea.Cmd, bool) {
	d, ok := msg.(dmOpenedMsg)
	if !ok {
		return nil, false
	}
	if d.err != nil {
		return m.say("couldn't open a conversation: "+d.err.Error(), true), true
	}
	m.setFocus(onCompose)
	return m.visit(d.conv), true
}

// until is when a status runs out, as it's told: the time if it's today.
func until(at int64, now time.Time) string {
	t := time.Unix(at, 0).In(now.Location())
	if sameDay(t, now) {
		return "until " + t.Format("15:04")
	}
	return "until " + strings.ToLower(t.Format("2 Jan"))
}

// overlayCard draws the card over the frame, which goes faint behind it.
func (m *Model) overlayCard(v store.View, frame []canvas.Row) []canvas.Row {
	ink, c := m.pal.Main, &m.ppl.card
	bw := min(m.w-4, 56)
	if bw < 36 {
		return frame
	}
	inner := bw - 4
	edge, fill := m.pal.Orange, ink.Text.Bg(ink.Sel.BG)
	p, now := v.Person(c.id), time.Now()
	if c.tz != p.TZ {
		c.tz, c.loc = p.TZ, nil
		if p.TZ != "" {
			c.loc, _ = time.LoadLocation(p.TZ)
		}
	}

	line := func(r canvas.Row) canvas.Row {
		return append(append(canvas.Row{canvas.T("│ ", edge.Bg(fill.BG))}, canvas.Fit(r, inner, fill)...), canvas.T(" │", edge.Bg(fill.BG)))
	}
	edgeRow := func(l, label, r string) canvas.Row {
		row := canvas.Row{canvas.T(l+"─ ", edge), canvas.T(label, ink.Sub.With(canvas.Bold))}
		return append(row, canvas.T(" "+strings.Repeat("─", max(0, bw-row.Width()-2))+r, edge))
	}
	dim, sub, bright := fill.Fg(ink.Dim.FG), fill.Fg(ink.Sub.FG), fill.Fg(ink.Bright.FG).With(canvas.Bold)

	// Beside the picture: who they are, what they go by, their job, if they're about.
	real := p.Real
	if real == "" {
		real = p.Name
	}
	goes := []string{}
	if p.Name != real {
		goes = append(goes, p.Name)
	}
	if p.Handle != "" {
		goes = append(goes, "@"+p.Handle)
	}
	if p.Pronouns != "" {
		goes = append(goes, p.Pronouns)
	}
	here := canvas.Row{canvas.T("presence unknown", dim)}
	switch v.Presence(c.id) {
	case "active":
		here = canvas.Row{canvas.T("● ", fill.Fg(m.pal.Green.FG)), canvas.T("active", sub)}
	case "away":
		here = canvas.Row{canvas.T("○ ", dim), canvas.T("away", sub)}
	}
	if p.Bot {
		here = canvas.Row{canvas.T("◇ ", dim), canvas.T("app", sub)}
	}
	side := []canvas.Row{
		{canvas.T(real, bright)},
		{canvas.T(strings.Join(goes, " · "), sub)},
		{canvas.T(p.Title, sub)},
		here,
	}

	box := []canvas.Row{edgeRow("╭", "Profile", "╮")}
	pic := m.cardAvatar(&p)
	for i := range cardAvatarH {
		r := append(pic[i], canvas.T("  ", fill))
		box = append(box, line(append(r, side[i]...)))
	}
	box = append(box, line(nil))
	if (p.StatusEmoji != "" || p.StatusText != "") && (p.StatusUntil == 0 || p.StatusUntil > now.Unix()) {
		e := ""
		if p.StatusEmoji != "" {
			e, _ = emojiText(strings.Trim(p.StatusEmoji, ":"))
		}
		row := canvas.Row{canvas.T(strings.TrimSpace(e+" "+p.StatusText), fill)}
		if p.StatusUntil > now.Unix() {
			row = append(row, canvas.T(" · "+until(p.StatusUntil, now), dim))
		}
		box = append(box, line(row))
	}
	if c.loc != nil {
		t := now.In(c.loc)
		row := canvas.Row{canvas.T(t.Format("15:04"), fill), canvas.T(" there · "+p.TZ, dim)}
		if !sameDay(t, now) {
			row = canvas.Row{canvas.T(strings.ToLower(t.Format("Mon 15:04")), fill), canvas.T(" there · "+p.TZ, dim)}
		}
		box = append(box, line(row))
	}
	if p.Email != "" {
		box = append(box, line(canvas.Row{canvas.T(p.Email, fill.Fg(m.pal.Blue.FG))}))
	}
	box = append(box, edgeRow("╰", "enter message · y copy @"+p.Handle+" · esc close", "╯"))

	out := make([]canvas.Row, len(frame))
	for i, r := range frame {
		out[i] = faint(r, ink.Faint.FG)
	}
	top, left := min(m.h/8, 4), (m.w-bw)/2
	for i, r := range box {
		if top+i < len(out)-1 {
			out[top+i] = canvas.Splice(out[top+i], left, r)
		}
	}
	return out
}

// cardAvatar is the person's picture as cardAvatarH rows cardAvatarW wide,
// once it's landed; until then, and without graphics, their initials on a
// tinted block.
func (m *Model) cardAvatar(p *store.Person) []canvas.Row {
	rows := make([]canvas.Row, cardAvatarH)
	if pic, ok := picture(p.Avatar, cardAvatarW, cardAvatarH); ok && pic.OK() {
		for i := range rows {
			if i < pic.Rows {
				rows[i] = canvas.Row{picSeg(pic, i), canvas.T(strings.Repeat(" ", cardAvatarW-pic.Cols), m.pal.Main.Text.Bg(m.pal.Main.Sel.BG))}
			}
		}
	}
	if rows[0] == nil {
		st := canvas.Style{}.Bg(tint(p.ID)).Fg(rgb(240, 236, 228)).With(canvas.Bold)
		for i := range rows {
			rows[i] = canvas.Row{canvas.T(strings.Repeat(" ", cardAvatarW), st)}
		}
		in := initials(p.Name)
		pad := (cardAvatarW - cellw.String(in)) / 2
		rows[1] = canvas.Row{canvas.T(fmt.Sprintf("%*s%s%*s", pad, "", in, cardAvatarW-pad-cellw.String(in), ""), st)}
	}
	for i := range rows {
		if rows[i] == nil { // a picture shorter than the block
			rows[i] = canvas.Row{canvas.T(strings.Repeat(" ", cardAvatarW), m.pal.Main.Text.Bg(m.pal.Main.Sel.BG))}
		}
	}
	return rows
}
