package ui

import (
	"cmp"
	"encoding/json/jsontext"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/0xdeafcafe/loafer/internal/mrkdwn"
	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/jsonx"
	"github.com/0xdeafcafe/photon/theme"
)

// Block Kit, legacy attachments and files, drawn read-only (docs/ui.md,
// Block Kit). They're decoded as a message is drawn, which is once per
// rowKey, so a frame never reads JSON. Each block and attachment is
// decoded on its own, so one Slack hasn't told us about costs only itself.

// textObj is Block Kit's text object. A bare string (a markdown block's
// text, or a context element's) reads as mrkdwn.
type textObj struct {
	Type string `json:"type"` // plain_text or mrkdwn
	Text string `json:"text"`
}

func (t *textObj) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		return jsonx.Unmarshal(b, &t.Text)
	}
	type plain textObj
	return jsonx.Unmarshal(b, (*plain)(t))
}

func (t *textObj) lines() []mrkdwn.Line {
	switch {
	case t == nil:
		return nil
	case t.Type == "plain_text":
		return mrkdwn.Plain(t.Text)
	}
	return mrkdwn.Parse(t.Text)
}

// flat is t as one line of plain text, for a chip's label.
func (t *textObj) flat(v store.View) string {
	var b strings.Builder
	for i, l := range t.lines() {
		if i > 0 {
			b.WriteByte(' ')
		}
		for _, s := range l.Spans {
			b.WriteString(spanText(v, s))
		}
	}
	return b.String()
}

type block struct {
	Type      string         `json:"type"`
	Text      *textObj       `json:"text"`      // header, section, markdown
	Fields    []textObj      `json:"fields"`    // section
	Accessory *element       `json:"accessory"` // section
	Elements  jsontext.Value `json:"elements"`  // actions and context: elements; rich_text: its parts
	AltText   string         `json:"alt_text"`  // image
	Title     *textObj       `json:"title"`     // image
	ImageURL  string         `json:"image_url"`
	// Slack adds these to image blocks it stores; seen in the web client's
	// payloads, not documented, so they may be missing.
	ImageWidth  int `json:"image_width"`
	ImageHeight int `json:"image_height"`
}

// element is an interactive element or a context's text or image; the
// rest of what pressing one needs is in press.go.
type element struct {
	Type          string   `json:"type"`
	Text          *textObj `json:"text"`
	URL           string   `json:"url"`
	Style         string   `json:"style"` // a button's primary or danger
	Placeholder   *textObj `json:"placeholder"`
	InitialOption *option  `json:"initial_option"`
	InitialDate   string   `json:"initial_date"`
	InitialTime   string   `json:"initial_time"`
	AltText       string   `json:"alt_text"`
	ImageURL      string   `json:"image_url"` // a context's image

	ActionID       string   `json:"action_id"`
	Value          string   `json:"value"`
	Options        []option `json:"options"`
	InitialOptions []option `json:"initial_options"` // checkboxes
	InitialValue   string   `json:"initial_value"`   // plain_text_input
	Multiline      bool     `json:"multiline"`
	OptionGroups   []struct {
		Options []option `json:"options"`
	} `json:"option_groups"`
}

// bodyRows is a message's text, or its blocks when it has them. Slack
// draws only the blocks then: the text is the notification's fallback,
// and a rich_text block says the same thing more exactly.
func bodyRows(p *Palette, v store.View, m *slack.Message, w int) []canvas.Row {
	if rows, ok := blockRows(p, v, m.Blocks, w); ok {
		return rows
	}
	if m.Text == "" {
		return nil // an attachment or a file, alone
	}
	return textRows(p, v, mrkdwn.Parse(m.Text), w)
}

// blockRows draws blocks w wide. ok is false when none of them could be
// drawn, so the text should be.
func blockRows(p *Palette, v store.View, raw jsontext.Value, w int) (out []canvas.Row, ok bool) {
	var bs []jsontext.Value
	if len(raw) == 0 || jsonx.Unmarshal(raw, &bs) != nil {
		return nil, false
	}
	for _, b := range bs {
		rows, drawn := blockOf(p, v, b, w, len(out) > 0)
		out = append(out, rows...)
		ok = ok || drawn
	}
	return out, ok
}

// blockOf draws one block; drawn is false when it couldn't be, and what
// came back is a line saying so. gap says there's something above.
func blockOf(p *Palette, v store.View, raw jsontext.Value, w int, gap bool) (rows []canvas.Row, drawn bool) {
	ink := p.Main
	var b block
	if jsonx.Unmarshal(raw, &b) != nil {
		return unsupported(p, "", w), false
	}
	switch b.Type {
	case "header":
		if gap {
			rows = append(rows, nil)
		}
		for _, l := range b.Text.lines() {
			rows = append(rows, canvas.Wrap(styled(p, v, l.Spans, ink.Bright.With(canvas.Bold)), w)...)
		}
	case "section":
		rows = textRows(p, v, b.Text.lines(), w)
		rows = append(rows, columns(p, len(b.Fields), func(int) bool { return true }, func(i, w int) []canvas.Row {
			return textRows(p, v, b.Fields[i].lines(), w)
		}, w)...)
		if b.Accessory != nil {
			rows = append(rows, canvas.Row{chip(p, v, *b.Accessory)})
		}
	case "context":
		var els []element
		if jsonx.Unmarshal(b.Elements, &els) != nil {
			return unsupported(p, b.Type, w), false
		}
		var r canvas.Row
		for _, e := range els {
			if e.Text == nil {
				if s, ok := inlinePic(e.ImageURL); ok {
					r = append(r, s)
				}
				continue
			}
			t := textObj{Type: e.Type, Text: e.Text.Text}
			for _, l := range t.lines() {
				if len(r) > 0 {
					r = append(r, canvas.T("  ", ink.Dim))
				}
				r = append(r, styled(p, v, l.Spans, ink.Dim)...)
			}
		}
		rows = canvas.Wrap(r, w)
	case "divider":
		rows = []canvas.Row{{canvas.T(strings.Repeat("─", w), ink.Faint)}}
	case "image":
		label := b.AltText
		if b.Title != nil {
			label = b.Title.flat(v)
		}
		rows = imageRows(p, b.ImageURL, label, b.ImageWidth, b.ImageHeight, w)
	case "actions":
		var els []element
		if jsonx.Unmarshal(b.Elements, &els) != nil {
			return unsupported(p, b.Type, w), false
		}
		rows = chipRows(p, v, els, w)
	case "rich_text":
		lines, ok := mrkdwn.RichText(b.Elements)
		if !ok {
			return unsupported(p, b.Type, w), false
		}
		rows = textRows(p, v, lines, w)
	case "markdown":
		rows = textRows(p, v, b.Text.lines(), w)
	default: // input, file, video, call, table, and whatever comes next
		return unsupported(p, b.Type, w), false
	}
	return rows, true
}

func unsupported(p *Palette, kind string, w int) []canvas.Row {
	t := "unsupported block"
	if kind != "" {
		t += " (" + kind + ")"
	}
	return canvas.Wrap(canvas.Row{canvas.T(t, p.Main.Faint)}, w)
}

// imageRows is an image: its picture where the terminal draws them, then
// "▣ alt 1200×800" as its caption (or all there is, where it doesn't).
func imageRows(p *Palette, src, label string, iw, ih, w int) []canvas.Row {
	t := "▣ " + cmp.Or(label, "image")
	if iw > 0 && ih > 0 {
		t += " " + strconv.Itoa(iw) + "×" + strconv.Itoa(ih)
	}
	return append(pictureRows(src, iw, ih, w), canvas.Wrap(canvas.Row{canvas.T(t, p.Main.Dim)}, w)...)
}

// chip is an interactive element as it reads: a button's label on its
// fill, a menu's choice with ▾. press.go finds them again to press.
func chip(p *Palette, v store.View, e element) canvas.Seg {
	switch e.Type {
	case "button":
		st := p.Chip
		switch e.Style {
		case "primary":
			st = p.Chip.Bg(p.Green.FG).Fg(p.Main.Ground)
		case "danger":
			st = p.Chip.Bg(p.Red.FG).Fg(p.Main.Ground)
		}
		label := " " + e.Text.flat(v) + " "
		if e.URL != "" {
			label += "↗ "
			st.Link = e.URL
		}
		return canvas.T(label, st)
	case "overflow":
		return canvas.T(" ⋯ ", p.Chip)
	case "image":
		return canvas.T("▣ "+cmp.Or(e.AltText, "image"), p.Main.Dim)
	}
	label := cmp.Or(e.InitialDate, e.InitialTime)
	switch {
	case e.InitialOption != nil:
		label = e.InitialOption.Text.flat(v)
	case label == "" && e.Placeholder != nil:
		label = e.Placeholder.flat(v)
	}
	return canvas.T(" "+cmp.Or(label, strings.ReplaceAll(e.Type, "_", " "))+" ▾ ", p.Chip)
}

// chipRows lays chips out two cells apart, starting a row when one
// won't fit.
func chipRows(p *Palette, v store.View, els []element, w int) []canvas.Row {
	var out []canvas.Row
	var r canvas.Row
	for _, e := range els {
		c := chip(p, v, e)
		if len(r) > 0 && r.Width()+2+c.W > w {
			out, r = append(out, r), nil
		}
		if len(r) > 0 {
			r = append(r, canvas.T("  ", p.Main.Text))
		}
		r = append(r, c)
	}
	if len(r) > 0 {
		out = append(out, r)
	}
	return out
}

// columns lays n cells out two to a row when there's room and both can
// share one (pair), else one under another. draw is cell i at a width.
func columns(p *Palette, n int, pair func(int) bool, draw func(i, w int) []canvas.Row, w int) []canvas.Row {
	col := (w - 2) / 2
	var out []canvas.Row
	for i := 0; i < n; i++ {
		if col < 20 || i+1 >= n || !pair(i) || !pair(i+1) {
			out = append(out, draw(i, w)...)
			continue
		}
		a, b := draw(i, col), draw(i+1, col)
		for k := range max(len(a), len(b)) {
			var l, r canvas.Row
			if k < len(a) {
				l = a[k]
			}
			if k < len(b) {
				r = b[k]
			}
			out = append(out, append(canvas.Fit(l, col+2, p.Main.Text), r...))
		}
		i++
	}
	return out
}

// attachment is a legacy attachment, as bots like Grafana still send, or
// a link's unfurl. Newer apps put blocks inside one for its colour bar.
type attachment struct {
	Color       string         `json:"color"`
	Pretext     string         `json:"pretext"`
	AuthorName  string         `json:"author_name"`
	AuthorLink  string         `json:"author_link"`
	ServiceName string         `json:"service_name"` // an unfurl's site
	Title       string         `json:"title"`
	TitleLink   string         `json:"title_link"`
	Text        string         `json:"text"`
	Fallback    string         `json:"fallback"`
	ImageURL    string         `json:"image_url"`
	ImageWidth  int            `json:"image_width"`
	ImageHeight int            `json:"image_height"`
	Footer      string         `json:"footer"`
	TS          jsontext.Value `json:"ts"` // a number, or a string from unfurled messages
	Blocks      jsontext.Value `json:"blocks"`
	Fields      []struct {
		Title string `json:"title"`
		Value string `json:"value"`
		Short bool   `json:"short"`
	} `json:"fields"`
}

// attachmentRows draws attachments behind a bar in their colour, the
// pretext above it as Slack has it.
func attachmentRows(p *Palette, v store.View, raw jsontext.Value, w int, now time.Time) []canvas.Row {
	var as []jsontext.Value
	if len(raw) == 0 || jsonx.Unmarshal(raw, &as) != nil {
		return nil
	}
	ink := p.Main
	iw := max(1, w-2)
	var out []canvas.Row
	for _, x := range as {
		var a attachment
		if jsonx.Unmarshal(x, &a) != nil {
			out = append(out, unsupported(p, "attachment", w)...)
			continue
		}
		if a.Pretext != "" {
			out = append(out, textRows(p, v, mrkdwn.Parse(a.Pretext), w)...)
		}
		var inner []canvas.Row
		add := func(text string, st *canvas.Style) {
			rows := textRows(p, v, mrkdwn.Parse(text), iw)
			if st != nil {
				for i := range rows {
					rows[i] = canvas.Restyle(rows[i], *st)
				}
			}
			inner = append(inner, rows...)
		}
		if who := cmp.Or(a.AuthorName, a.ServiceName); who != "" {
			st := ink.Sub.With(canvas.Bold)
			st.Link = a.AuthorLink
			add(who, &st)
		}
		if a.Title != "" {
			st := p.Blue.With(canvas.Bold)
			st.Link = a.TitleLink
			add(a.Title, &st)
		}
		if a.Text != "" {
			add(a.Text, nil)
		} else if a.Title == "" && len(a.Fields) == 0 && len(a.Blocks) == 0 {
			add(a.Fallback, nil)
		}
		inner = append(inner, columns(p, len(a.Fields), func(i int) bool { return a.Fields[i].Short }, func(i, w int) []canvas.Row {
			f := a.Fields[i]
			rows := canvas.Wrap(canvas.Row{canvas.T(f.Title, ink.Text.With(canvas.Bold))}, w)
			return append(rows, textRows(p, v, mrkdwn.Parse(f.Value), w)...)
		}, iw)...)
		if a.ImageURL != "" {
			inner = append(inner, imageRows(p, a.ImageURL, imageName(a.ImageURL), a.ImageWidth, a.ImageHeight, iw)...)
		}
		if rows, ok := blockRows(p, v, a.Blocks, iw); ok {
			inner = append(inner, rows...)
		}
		foot := a.Footer
		if ts := strings.Trim(string(a.TS), `"`); ts != "" && ts != "0" && ts != "null" {
			foot = strings.TrimPrefix(foot+" · "+clock(tsTime(ts), now), " · ")
		}
		if foot != "" {
			add(foot, &ink.Dim)
		}

		bar := canvas.T("▌ ", ink.Faint)
		if c, ok := hexRGB(a.Color); ok {
			bar = canvas.T("▌ ", ink.Text.Fg(c))
		}
		for _, r := range inner {
			out = append(out, append(canvas.Row{bar}, r...))
		}
	}
	return out
}

// imageName is what an image's URL calls it.
func imageName(u string) string {
	if x, err := url.Parse(u); err == nil && path.Base(x.Path) != "/" && path.Base(x.Path) != "." {
		return path.Base(x.Path)
	}
	return "image"
}

// hexRGB reads Slack's attachment colour ("#36a64f", "36a64f", or a name).
func hexRGB(s string) (theme.RGB, bool) {
	switch s {
	case "good":
		return rgb(46, 182, 125), true
	case "warning":
		return rgb(236, 178, 46), true
	case "danger":
		return rgb(224, 30, 90), true
	}
	s = strings.TrimPrefix(s, "#")
	n, err := strconv.ParseUint(s, 16, 32)
	if len(s) != 6 || err != nil {
		return theme.RGB{}, false
	}
	return rgb(uint8(n>>16), uint8(n>>8), uint8(n)), true
}

// file is an uploaded file, as a message carries it.
type file struct {
	Name       string `json:"name"`
	Title      string `json:"title"`
	Mimetype   string `json:"mimetype"`
	PrettyType string `json:"pretty_type"`
	Size       int64  `json:"size"`
	Permalink  string `json:"permalink"`
	Mode       string `json:"mode"` // hosted, external, snippet, tombstone, hidden_by_limit
}

// fileRows is a line per file: its kind, its name (a link to it in
// Slack), type and size. The images lane draws image files here too.
func fileRows(p *Palette, raw jsontext.Value, w int) []canvas.Row {
	var fs []jsontext.Value
	if len(raw) == 0 || jsonx.Unmarshal(raw, &fs) != nil {
		return nil
	}
	ink := p.Main
	var out []canvas.Row
	for _, x := range fs {
		var f file
		if jsonx.Unmarshal(x, &f) != nil {
			continue
		}
		var r canvas.Row
		switch f.Mode {
		case "tombstone":
			r = canvas.Row{canvas.T("▤ this file was deleted", ink.Faint)}
		case "hidden_by_limit":
			r = canvas.Row{canvas.T("▤ hidden, past the workspace's history limit", ink.Faint)}
		default:
			icon := "▤ "
			switch strings.Split(f.Mimetype, "/")[0] {
			case "image":
				icon = "▣ "
				out = append(out, filePicture(x, w)...)
			case "video":
				icon = "▶ "
			case "audio":
				icon = "♪ "
			}
			name := p.Blue.With(canvas.Underline)
			name.Link = f.Permalink
			r = canvas.Row{canvas.T(icon, ink.Dim), canvas.T(cmp.Or(f.Title, f.Name, "file"), name)}
			for _, s := range []string{f.PrettyType, size(f.Size)} {
				if s != "" {
					r = append(r, canvas.T(" · "+s, ink.Dim))
				}
			}
		}
		out = append(out, canvas.Wrap(r, w)...)
	}
	return out
}

// size is a file's size as people say it.
func size(n int64) string {
	const k = 1024
	switch {
	case n <= 0:
		return ""
	case n < k:
		return strconv.FormatInt(n, 10) + " B"
	case n < k*k:
		return strconv.FormatFloat(float64(n)/k, 'f', 0, 64) + " KB"
	case n < k*k*k:
		return strconv.FormatFloat(float64(n)/(k*k), 'f', 1, 64) + " MB"
	}
	return strconv.FormatFloat(float64(n)/(k*k*k), 'f', 1, 64) + " GB"
}
