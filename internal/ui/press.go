package ui

import (
	"encoding/json/jsontext"
	"log/slog"
	"maps"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/jsonx"
)

// Block Kit's elements, pressed from the message cursor (docs/ui.md,
// Block Kit): b steps through the selected message's buttons and menus,
// the chosen one lit orange, and enter presses it. A button tells the app
// at once; a menu or overflow opens a chooser first, and a datepicker
// takes the date typed. What the app does about it comes down the
// websocket: a modal (modal.go), or an edit to the message.

// option is a menu's choice.
type option struct {
	Text  textObj `json:"text"`
	Value string  `json:"value"`
}

// target is an element drawn as a chip, and the block it's in.
type target struct {
	block string
	el    element
}

// blockKit is the model's share of it: the chosen element, the chooser
// and the modal.
type blockKit struct {
	ts   string // the message the chosen element is in; it lapses when the cursor leaves it
	at   int
	seg  canvas.Seg // the chosen one as drawn, to find it in the rows
	nth  int        // how many drawn like it come before it
	pick chooser
	md   modalUI
}

type pressedMsg struct{ err error }

// targets is what's drawn as a chip in blocks, in the order it's drawn:
// actions' elements and sections' accessories. Each block is read on its
// own, as blockRows does.
func targets(raw jsontext.Value) []target {
	var bs []jsontext.Value
	if len(raw) == 0 || jsonx.Unmarshal(raw, &bs) != nil {
		return nil
	}
	var out []target
	for _, raw := range bs {
		var b struct {
			Type      string         `json:"type"`
			BlockID   string         `json:"block_id"`
			Elements  jsontext.Value `json:"elements"`
			Accessory *element       `json:"accessory"`
		}
		if jsonx.Unmarshal(raw, &b) != nil {
			continue
		}
		var els []element
		switch {
		case b.Type == "actions":
			if jsonx.Unmarshal(b.Elements, &els) != nil {
				continue
			}
		case b.Type == "section" && b.Accessory != nil:
			els = []element{*b.Accessory}
		}
		for _, e := range els {
			if e.Type != "image" {
				out = append(out, target{b.BlockID, e})
			}
		}
	}
	return out
}

// options is a menu's choices, its groups run together.
func (e *element) options() []option {
	out := e.Options
	for _, g := range e.OptionGroups {
		out = append(out, g.Options...)
	}
	return out
}

// pressKey is b, shift+b, and enter and esc while an element is chosen.
func (m *Model) pressKey(s string) (tea.Cmd, bool) {
	k := &m.kit
	on := k.ts != "" && k.ts == m.sel
	switch {
	case s == "b":
		return m.chipStep(1), true
	case s == "B":
		return m.chipStep(-1), true
	case on && s == "enter":
		return m.press(), true
	case on && s == "esc":
		k.ts = ""
		return nil, true
	}
	return nil, false
}

// chipStep chooses the selected message's next element, d away.
func (m *Model) chipStep(d int) tea.Cmd {
	msg, ok := m.selected()
	if !ok {
		return m.say("pick a message first (↑)", false)
	}
	k := &m.kit
	els := targets(msg.Blocks) // again each time: the app may have changed them
	switch n := len(els); {
	case n == 0:
		k.ts = ""
		return m.say("nothing to press in that one", false)
	case k.ts != msg.TS && d < 0:
		k.at = n - 1
	case k.ts != msg.TS:
		k.at = 0
	default:
		k.at = (min(k.at, n-1) + d + n) % n
	}
	k.ts = msg.TS
	m.st.Read(func(v store.View) {
		k.seg, k.nth = chip(&m.pal, v, els[k.at].el), 0
		for _, t := range els[:k.at] {
			if c := chip(&m.pal, v, t.el); c.Text == k.seg.Text && c.St == k.seg.St {
				k.nth++
			}
		}
	})
	return nil
}

// lit is the selected message's rows with its chosen element lit; rows
// are picked's fresh copy, so it's changed in place.
func (m *Model) lit(ts string, rows []canvas.Row) []canvas.Row {
	k := &m.kit
	if k.ts != ts {
		return rows
	}
	n := k.nth
	for _, r := range rows {
		for j, s := range r {
			if s.Text != k.seg.Text || s.St != k.seg.St {
				continue
			}
			if n == 0 {
				r[j].St = s.St.Bg(m.pal.Orange.FG).Fg(m.pal.Main.Ground).With(canvas.Bold)
				return rows
			}
			n--
		}
	}
	return rows
}

// press works the chosen element.
func (m *Model) press() tea.Cmd {
	k := &m.kit
	msg, ok := m.selected()
	els := targets(msg.Blocks)
	if !ok || k.at >= len(els) {
		return nil
	}
	t := els[k.at]
	switch t.el.Type {
	case "button":
		cmd := m.act(msg, t, nil)
		if u, err := url.Parse(t.el.URL); err == nil && (u.Scheme == "https" || u.Scheme == "http") {
			target := u.String()
			cmd = tea.Batch(cmd, func() tea.Msg {
				if err := exec.Command("open", target).Run(); err != nil {
					slog.Warn("open link", "err", err)
				}
				return nil
			})
		}
		return cmd
	case "overflow", "static_select":
		m.choose(t.el.options(), func(o option) tea.Cmd {
			return m.act(msg, t, map[string]any{"selected_option": o})
		})
	case "datepicker":
		m.chooseDate(t.el.InitialDate, func(date string) tea.Cmd {
			return m.act(msg, t, map[string]any{"selected_date": date})
		})
	default:
		return m.say("loafer can't work a "+strings.ReplaceAll(t.el.Type, "_", " ")+" yet", false)
	}
	return nil
}

// act tells msg's app that t was pressed, with what was chosen.
// UNCERTAIN: the action's keys are the public block_actions payload's.
func (m *Model) act(msg slack.Message, t target, chose map[string]any) tea.Cmd {
	p := map[string]any{"type": t.el.Type, "action_id": t.el.ActionID, "block_id": t.block,
		"action_ts": strconv.FormatFloat(float64(time.Now().UnixMicro())/1e6, 'f', 6, 64)}
	if t.el.Value != "" {
		p["value"] = t.el.Value
	}
	if t.el.Text != nil {
		p["text"] = t.el.Text
	}
	maps.Copy(p, chose)
	a := slack.Action{ServiceID: msg.BotID, AppID: msg.AppID, Channel: m.open, TS: msg.TS, Token: slack.ClientToken(), Payload: p}
	label := ""
	m.st.Read(func(v store.View) {
		a.TeamID = v.Team().ID
		label = chip(&m.pal, v, t.el).Text
	})
	m.st.ExpectView(a.Token)
	return tea.Batch(m.say("✻ "+strings.TrimSpace(label), false), func() tea.Msg {
		return pressedMsg{m.api.BlockAction(m.ctx, a)}
	})
}

// --- the chooser: a menu's options, or a date typed ---

type chooser struct {
	on    bool
	opts  []option
	at    int
	date  bool   // typing a date, not choosing
	typed []rune // the date
	pick  func(option) tea.Cmd
	day   func(string) tea.Cmd
}

func (m *Model) choose(opts []option, pick func(option) tea.Cmd) {
	if len(opts) > 0 {
		m.kit.pick = chooser{on: true, opts: opts, pick: pick}
	}
}

func (m *Model) chooseDate(initial string, day func(string) tea.Cmd) {
	m.kit.pick = chooser{on: true, date: true, typed: []rune(initial), day: day}
}

// blockKey takes the keys while the chooser or a modal is up.
func (m *Model) blockKey(k tea.KeyPressMsg) tea.Cmd {
	if m.kit.pick.on {
		return m.chooseKey(k)
	}
	return m.modalKey(k)
}

func (m *Model) chooseKey(k tea.KeyPressMsg) tea.Cmd {
	c := &m.kit.pick
	s := k.String()
	switch {
	case s == "esc":
		c.on = false
	case s == "enter" && c.date:
		d := string(c.typed)
		if _, err := time.Parse(time.DateOnly, d); err != nil {
			return m.say("a date as 2026-10-09", true)
		}
		c.on = false
		return c.day(d)
	case s == "enter":
		c.on = false
		return c.pick(c.opts[c.at])
	case c.date && s == "backspace":
		if len(c.typed) > 0 {
			c.typed = c.typed[:len(c.typed)-1]
		}
	case c.date:
		c.typed = append(c.typed, []rune(k.Text)...)
	case s == "up" || s == "k" || s == "ctrl+p":
		c.at = (c.at + len(c.opts) - 1) % len(c.opts)
	case s == "down" || s == "j" || s == "ctrl+n":
		c.at = (c.at + 1) % len(c.opts)
	}
	return nil
}

// overlayChooser draws the chooser over the frame.
func (m *Model) overlayChooser(v store.View, frame []canvas.Row) []canvas.Row {
	ink, c := m.pal.Main, &m.kit.pick
	bw := min(m.w-4, 40)
	if bw < 24 {
		return frame
	}
	fill := ink.Text.Bg(ink.Sel.BG)
	var body []canvas.Row
	foot := "↑↓ choose · enter pick · esc close"
	if c.date {
		body = []canvas.Row{{canvas.T("❯ ", fill.Fg(m.pal.Orange.FG).With(canvas.Bold)), canvas.T(string(c.typed), fill.Fg(ink.Bright.FG)), canvas.T("▏", fill.Fg(m.pal.Orange.FG))}}
		if len(c.typed) == 0 {
			body[0] = append(body[0], canvas.T("yyyy-mm-dd", fill.Fg(ink.Faint.FG)))
		}
		foot = "enter pick · esc close"
	}
	listH := max(1, min(len(c.opts), m.h/2))
	from := max(0, c.at-listH+1)
	for i := from; i < min(len(c.opts), from+listH); i++ {
		base, mark := fill, canvas.T("  ", fill)
		if i == c.at {
			base = fill.Bg(ink.Hover.BG)
			mark = canvas.T("▍ ", base.Fg(m.pal.Orange.FG))
		}
		body = append(body, canvas.Fit(canvas.Row{mark, canvas.T(c.opts[i].Text.flat(v), base)}, bw-4, base))
	}
	title := "▾ Choose"
	if c.date {
		title = "▾ Date"
	}
	return m.float(frame, m.boxed(title, foot, body, bw, m.pal.Orange, fill), min(m.h/4, 6))
}

// boxed is body in a rounded edge bw wide, title set into the top and
// foot into the bottom, as the other overlays have it.
func (m *Model) boxed(title, foot string, body []canvas.Row, bw int, edge, fill canvas.Style) []canvas.Row {
	ink := m.pal.Main
	inner := bw - 4
	edgeRow := func(l, label, r string) canvas.Row {
		row := canvas.Row{canvas.T(l+"─ ", edge), canvas.T(label, ink.Sub.With(canvas.Bold))}
		return append(row, canvas.T(" "+strings.Repeat("─", max(0, bw-row.Width()-2))+r, edge))
	}
	out := []canvas.Row{edgeRow("╭", title, "╮")}
	for _, r := range body {
		out = append(out, append(append(canvas.Row{canvas.T("│ ", edge.Bg(fill.BG))}, canvas.Fit(r, inner, fill)...), canvas.T(" │", edge.Bg(fill.BG))))
	}
	return append(out, edgeRow("╰", foot, "╯"))
}

// float lays box over the frame, top rows down and centred, with the
// frame faint behind it.
func (m *Model) float(frame, box []canvas.Row, top int) []canvas.Row {
	out := make([]canvas.Row, len(frame))
	for i, r := range frame {
		out[i] = faint(r, m.pal.Main.Faint.FG)
	}
	if len(box) == 0 {
		return out
	}
	left := max(0, (m.w-box[0].Width())/2)
	for i, r := range box {
		if top+i < len(out)-1 {
			out[top+i] = canvas.Splice(out[top+i], left, r)
		}
	}
	return out
}

// overlayBlocks draws the modal, then the chooser over it.
func (m *Model) overlayBlocks(v store.View, frame []canvas.Row) []canvas.Row {
	if m.kit.md.on {
		frame = m.overlayModal(v, frame)
	}
	if m.kit.pick.on {
		frame = m.overlayChooser(v, frame)
	}
	return frame
}

// pressed takes Slack's answer to a press or a submit; the ✻ said while
// it was on its way goes.
func (m *Model) pressed(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case pressedMsg:
		if msg.err != nil {
			return m.say("✗ couldn't press it: "+msg.err.Error(), true)
		}
	case submittedMsg:
		m.kit.md.busy = false
		if msg.err != nil {
			return m.say("✗ couldn't submit: "+msg.err.Error(), true)
		}
	}
	if strings.HasPrefix(m.flash, "✻ ") {
		m.flash = ""
	}
	return nil
}
