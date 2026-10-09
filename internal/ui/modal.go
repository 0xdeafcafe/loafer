package ui

import (
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/jsonx"
)

// An app's modal (docs/ui.md, Modal). The store holds the views Slack
// pushed (store/modal.go); this draws the top one as a sheet over the
// screen and keeps what's typed into it. tab moves between the inputs and
// the buttons, ctrl+enter submits, and leaving with edits asks first.
// Blocks that aren't inputs are drawn as a message's are, once a width.

type modalUI struct {
	on                   bool
	seen                 *store.Modal // what it was built from, compared and never read
	id, hash             string
	view                 jsontext.Value
	title, submit, close string
	parts                []part
	fields               []field
	errs                 map[string]string // by block_id
	at                   int               // the focus: a field, then submit, then close
	edited, leaving      bool              // leaving: asking "keep editing / leave"
	busy                 bool              // a submit is on its way
	top                  int               // body rows scrolled away
	drawnW               int               // the width the parts' rows are drawn at
}

// part is a block of the view: an input, or anything else drawn.
type part struct {
	raw   jsontext.Value
	rows  []canvas.Row
	field int // the input it is, or -1
}

// field is an input and what's in it.
type field struct {
	block, action, kind, label, hint, placeholder string
	optional, multi                               bool
	opts                                          []option
	text                                          []rune // plain_text_input and datepicker
	on                                            []bool // the options ticked, or the one chosen
	cur                                           int    // the option under the cursor
}

type submittedMsg struct{ err error }

// syncModal follows the store's top modal, keeping what's been typed when
// it's the same view come again.
func (m *Model) syncModal() {
	md := &m.kit.md
	m.st.Read(func(v store.View) {
		top := v.Modal()
		switch {
		case top == md.seen:
			return
		case top == nil:
			*md = modalUI{}
			return
		}
		md.seen, md.errs = top, top.Errors
		same := md.on && md.id == top.ID
		if same && bytes.Equal(md.view, top.View) {
			return // only its errors changed
		}
		old := md.fields
		m.buildModal(v, top)
		if !same {
			md.at, md.top, md.edited, md.leaving, md.busy = 0, 0, false, false, false
			return
		}
		for i, f := range md.fields {
			if j := slices.IndexFunc(old, func(o field) bool { return o.block == f.block && o.action == f.action && o.kind == f.kind }); j >= 0 && len(old[j].on) == len(f.on) {
				md.fields[i].text, md.fields[i].on = old[j].text, old[j].on
			}
		}
		md.at = min(md.at, len(md.fields)+1)
	})
}

// buildModal reads the view's title, buttons and blocks.
func (m *Model) buildModal(v store.View, top *store.Modal) {
	md := &m.kit.md
	var view struct {
		Title  *textObj         `json:"title"`
		Submit *textObj         `json:"submit"`
		Close  *textObj         `json:"close"`
		Blocks []jsontext.Value `json:"blocks"`
	}
	_ = jsonx.Unmarshal(top.View, &view) // what decodes is drawn
	md.on, md.id, md.hash, md.view, md.drawnW = true, top.ID, top.Hash, top.View, 0
	md.title = cmp.Or(view.Title.flat(v), "app")
	md.submit, md.close = "", "Cancel"
	if view.Submit != nil {
		md.submit = view.Submit.flat(v)
	}
	if view.Close != nil {
		md.close = view.Close.flat(v)
	}
	md.parts, md.fields = nil, nil
	for _, raw := range view.Blocks {
		var b struct {
			Type     string   `json:"type"`
			BlockID  string   `json:"block_id"`
			Label    *textObj `json:"label"`
			Hint     *textObj `json:"hint"`
			Optional bool     `json:"optional"`
			Element  *element `json:"element"`
		}
		_ = jsonx.Unmarshal(raw, &b)
		e := b.Element
		if b.Type != "input" || e == nil || !slices.Contains([]string{"plain_text_input", "static_select", "checkboxes", "radio_buttons", "datepicker"}, e.Type) {
			md.parts = append(md.parts, part{raw: raw, field: -1})
			continue
		}
		f := field{block: b.BlockID, action: e.ActionID, kind: e.Type, label: b.Label.flat(v), hint: b.Hint.flat(v),
			optional: b.Optional, multi: e.Multiline, opts: e.options()}
		if e.Placeholder != nil {
			f.placeholder = e.Placeholder.flat(v)
		}
		f.on = make([]bool, len(f.opts))
		for i, o := range f.opts {
			f.on[i] = (e.InitialOption != nil && e.InitialOption.Value == o.Value) ||
				slices.ContainsFunc(e.InitialOptions, func(x option) bool { return x.Value == o.Value })
		}
		f.text = []rune(cmp.Or(e.InitialValue, e.InitialDate))
		md.parts = append(md.parts, part{field: len(md.fields)})
		md.fields = append(md.fields, f)
	}
}

// --- keys ---

func (m *Model) modalKey(k tea.KeyPressMsg) tea.Cmd {
	md := &m.kit.md
	s := k.String()
	if md.leaving {
		switch s {
		case "y":
			return m.leaveModal()
		case "n", "esc":
			md.leaving = false
		}
		return nil
	}
	press := s == "enter" || s == "space"
	var f *field
	if md.at < len(md.fields) {
		f = &md.fields[md.at]
	}
	switch {
	case s == "tab":
		md.move(1)
	case s == "shift+tab":
		md.move(-1)
	case s == "esc" || (press && md.at == len(md.fields)+1):
		if md.edited {
			md.leaving = true
			return nil
		}
		return m.leaveModal()
	case s == "ctrl+enter" || s == "ctrl+s" || (press && md.at == len(md.fields)):
		return m.submitModal()
	case f == nil:
	case f.kind == "static_select" && press:
		m.choose(f.opts, func(o option) tea.Cmd {
			for i := range f.on {
				f.on[i] = f.opts[i].Value == o.Value
			}
			md.edited = true
			return nil
		})
	case f.kind == "checkboxes" || f.kind == "radio_buttons":
		switch {
		case len(f.opts) == 0:
		case s == "up" || s == "k":
			f.cur = (f.cur + len(f.opts) - 1) % len(f.opts)
		case s == "down" || s == "j":
			f.cur = (f.cur + 1) % len(f.opts)
		case press:
			was := f.on[f.cur]
			if f.kind == "radio_buttons" {
				clear(f.on)
			}
			f.on[f.cur], md.edited = !was || f.kind == "radio_buttons", true
		}
	case f.kind == "static_select":
	case s == "enter" && f.multi:
		f.text, md.edited = append(f.text, '\n'), true
	case s == "enter":
		md.move(1)
	case s == "backspace":
		if len(f.text) > 0 {
			f.text, md.edited = f.text[:len(f.text)-1], true
		}
	case k.Text != "":
		// shortcut: typing and backspace at the end only; a cursor that
		// moves comes if long answers need fixing in the middle.
		f.text, md.edited = append(f.text, []rune(k.Text)...), true
	}
	return nil
}

// move steps the focus d along the fields and buttons, past a submit
// button the view hasn't got.
func (md *modalUI) move(d int) {
	n := len(md.fields) + 2
	md.at = (md.at + d + n) % n
	if md.at == len(md.fields) && md.submit == "" {
		md.at = (md.at + d + n) % n
	}
}

// leaveModal lets the modal go, here at once and at Slack after.
func (m *Model) leaveModal() tea.Cmd {
	id := m.kit.md.id
	m.st.CloseModal(id)
	m.syncModal()
	return func() tea.Msg { return pressedMsg{m.api.CloseView(m.ctx, id)} }
}

// submitModal sends the inputs, once those that must be filled are.
// What the app says is wrong comes back through the store.
func (m *Model) submitModal() tea.Cmd {
	md := &m.kit.md
	if md.busy || md.submit == "" {
		return nil
	}
	errs := map[string]string{}
	for _, f := range md.fields {
		empty := len(strings.TrimSpace(string(f.text))) == 0 && !slices.Contains(f.on, true)
		if _, err := time.Parse(time.DateOnly, string(f.text)); f.kind == "datepicker" && !empty && err != nil {
			errs[f.block] = "a date as 2026-10-09"
		}
		if empty && !f.optional {
			errs[f.block] = "this one's needed"
		}
	}
	if len(errs) > 0 {
		m.st.ModalErrors(md.id, errs)
		m.syncModal()
		return nil
	}
	md.busy = true
	id, hash, vals := md.id, md.hash, md.values()
	return tea.Batch(m.say("✻ submitting", false), func() tea.Msg {
		r, err := m.api.SubmitView(m.ctx, id, hash, slack.ClientToken(), vals)
		switch {
		case err != nil:
		case len(r.Errors) > 0:
			m.st.ModalErrors(id, r.Errors)
		case r.Action == "update" || r.Action == "push":
			// the view that follows comes down the websocket
		default:
			// shortcut: "clear" closes this one, not those under it too.
			m.st.CloseModal(id)
		}
		return submittedMsg{err}
	})
}

// values is the inputs as Slack's state.values: block_id, then
// action_id, then the input's type and value.
func (md *modalUI) values() map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, f := range md.fields {
		v := map[string]any{"type": f.kind}
		var chosen []option
		for i, on := range f.on {
			if on {
				chosen = append(chosen, f.opts[i])
			}
		}
		text := any(nil)
		if len(f.text) > 0 {
			text = string(f.text)
		}
		switch f.kind {
		case "plain_text_input":
			v["value"] = text
		case "datepicker":
			v["selected_date"] = text
		case "checkboxes":
			v["selected_options"] = append([]option{}, chosen...)
		default:
			v["selected_option"] = nil
			if len(chosen) > 0 {
				v["selected_option"] = chosen[0]
			}
		}
		if out[f.block] == nil {
			out[f.block] = map[string]any{}
		}
		out[f.block][f.action] = v
	}
	return out
}

// --- drawing ---

// overlayModal draws the modal as rush's sheet: at most 72 wide, on the
// panel ground, its body scrolled to keep the focus in view and its
// buttons pinned at the foot.
func (m *Model) overlayModal(v store.View, frame []canvas.Row) []canvas.Row {
	md, ink, fill := &m.kit.md, m.pal.Main, m.pal.Panel
	bw := min(m.w-4, 72)
	if bw < 24 {
		return frame
	}
	inner := bw - 4
	if md.drawnW != inner {
		for i := range md.parts {
			p := &md.parts[i]
			if p.field >= 0 {
				continue
			}
			p.rows, _ = blockOf(&m.pal, v, p.raw, inner, i > 0)
			for j, r := range p.rows {
				r = slices.Clone(r)
				for k, s := range r {
					if s.St.BG == ink.Text.BG {
						r[k].St = s.St.Bg(fill.BG)
					}
				}
				p.rows[j] = r
			}
		}
		md.drawnW = inner
	}

	var body []canvas.Row
	from, to := 0, 0 // the focused field's rows
	for i, p := range md.parts {
		if p.field < 0 {
			body = append(body, p.rows...)
			continue
		}
		if i > 0 {
			body = append(body, nil)
		}
		if p.field == md.at {
			from = len(body)
		}
		body = append(body, m.fieldRows(&md.fields[p.field], p.field == md.at, md.errs[md.fields[p.field].block], inner)...)
		if p.field == md.at {
			to = len(body)
		}
	}
	top := min(m.h/8, 3)
	most := max(3, m.h-top-7)
	if md.at >= len(md.fields) {
		from, to = len(body), len(body)
	}
	md.top = min(max(md.top, to-most), from, max(0, len(body)-most))
	body = body[md.top:min(len(body), md.top+most)]

	button := func(label string, at int, st canvas.Style) canvas.Seg {
		if md.at == at {
			st = st.Bg(m.pal.Orange.FG).Fg(ink.Ground).With(canvas.Bold)
		}
		return canvas.T(" "+label+" ", st)
	}
	buttons := canvas.Row{button(md.close, len(md.fields)+1, m.pal.Chip)}
	if md.submit != "" {
		buttons = append(buttons, canvas.T("  ", fill), button(md.submit, len(md.fields), m.pal.Chip.Bg(m.pal.Green.FG).Fg(ink.Ground)))
	}
	body = append(body, nil, rightAlign(nil, buttons, inner, fill))

	edge, foot := m.pal.Orange, "tab next · ctrl+enter submit · esc close"
	if md.leaving {
		edge, foot = m.pal.Yellow, "y leave · n keep editing"
		body = append(body, canvas.Row{canvas.T("leave this form? you'll lose what you've entered", fill.Fg(m.pal.Yellow.FG).With(canvas.Bold))})
	}
	return m.float(frame, m.boxed(md.title, foot, body, bw, edge, fill), top)
}

// fieldRows is an input as it's drawn: its label, the input, its hint
// and what's wrong with it.
func (m *Model) fieldRows(f *field, focused bool, err string, w int) []canvas.Row {
	ink, fill, orange := m.pal.Main, m.pal.Panel, m.pal.Orange
	head := canvas.Row{canvas.T(f.label, fill.Fg(ink.Bright.FG).With(canvas.Bold))}
	if f.optional {
		head = append(head, canvas.T(" (optional)", fill.Fg(ink.Dim.FG)))
	}
	out := canvas.Wrap(head, w)
	mark := func(on bool) canvas.Seg {
		if on {
			return canvas.T("▍", fill.Fg(orange.FG))
		}
		return canvas.T(" ", fill)
	}
	switch f.kind {
	case "plain_text_input", "datepicker":
		box := m.pal.Input
		var r canvas.Row
		if len(f.text) > 0 {
			r = canvas.Row{canvas.T(string(f.text), box.Fg(ink.Bright.FG))}
		}
		if focused {
			r = append(r, canvas.T("▏", box.Fg(orange.FG)))
		}
		if len(f.text) == 0 {
			hold := f.placeholder
			if f.kind == "datepicker" {
				hold = cmp.Or(hold, "yyyy-mm-dd")
			}
			r = append(r, canvas.T(hold, box.Fg(ink.Faint.FG)))
		}
		for _, l := range splitLines(r) {
			for _, l := range canvas.Wrap(l, w-2) {
				out = append(out, append(canvas.Row{mark(focused), canvas.T(" ", fill)}, canvas.Fit(l, w-2, box)...))
			}
		}
	case "static_select":
		label := cmp.Or(f.placeholder, "choose")
		if i := slices.Index(f.on, true); i >= 0 {
			label = f.opts[i].Text.Text
		}
		st := m.pal.Chip
		if focused {
			st = st.Bg(orange.FG).Fg(ink.Ground).With(canvas.Bold)
		}
		out = append(out, canvas.Row{mark(focused), canvas.T(" ", fill), canvas.T(" "+label+" ▾ ", st)})
	default: // checkboxes, radio_buttons
		off, on := "☐ ", "☑ "
		if f.kind == "radio_buttons" {
			off, on = "○ ", "◉ "
		}
		for i, o := range f.opts {
			g := canvas.T(off, fill.Fg(ink.Dim.FG))
			if f.on[i] {
				g = canvas.T(on, fill.Fg(orange.FG))
			}
			out = append(out, canvas.Row{mark(focused && f.cur == i), canvas.T(" ", fill), g, canvas.T(o.Text.Text, fill.Fg(ink.Text.FG))})
		}
	}
	if f.hint != "" {
		out = append(out, canvas.Wrap(canvas.Row{canvas.T(f.hint, fill.Fg(ink.Dim.FG))}, w)...)
	}
	if err != "" {
		out = append(out, canvas.Wrap(canvas.Row{canvas.T("! "+err, fill.Fg(m.pal.Red.FG).With(canvas.Bold))}, w)...)
	}
	return out
}
