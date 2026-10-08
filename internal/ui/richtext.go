package ui

import (
	"strings"

	"github.com/0xdeafcafe/loafer/internal/mrkdwn"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/cellw"
	"github.com/0xdeafcafe/photon/hl"
)

// A message's text, laid out (docs/ui.md, Main screen). Most of it is
// rush's transcript markdown (convo/render.go) redone as rows: headings
// bold with a gap above, lists hanging under their bullets, tables in
// columns that shrink the widest first, and code on a panel, highlighted
// when the fence names a language.

var bullets = [...]string{"•", "◦", "▪"}

// textRows lays out a message's lines in w cells.
func textRows(p *Palette, v store.View, lines []mrkdwn.Line, w int) []canvas.Row {
	ink := p.Main
	var out []canvas.Row
	for i := 0; i < len(lines); {
		l := lines[i]
		// Blocks run over consecutive lines of their kind.
		end := i + 1
		for end < len(lines) && sameBlock(l, lines[end]) {
			end++
		}
		switch {
		case l.Pre:
			out = append(out, codeRows(p, lines[i:end], w)...)
		case l.Cells != nil:
			out = append(out, tableRows(p, v, lines[i:end], w)...)
		case l.Heading > 0:
			if len(out) > 0 {
				out = append(out, nil)
			}
			st := ink.Bright.With(canvas.Bold)
			if l.Heading > 2 {
				st = ink.Sub.With(canvas.Bold)
			}
			out = append(out, canvas.Wrap(styled(p, v, l.Spans, st), w)...)
		case l.Rule:
			out = append(out, canvas.Row{canvas.T(strings.Repeat("─", w), ink.Faint)})
		case l.Bullet != "":
			out = append(out, listRows(p, v, l, w)...)
		default:
			out = append(out, lineRows(p, v, l, w)...)
		}
		i = end
	}
	return out
}

// sameBlock says whether b carries on the block a started: the same code
// block, or more of a table.
func sameBlock(a, b mrkdwn.Line) bool {
	switch {
	case a.Pre:
		return b.Pre && !b.Start
	case a.Cells != nil:
		return b.Cells != nil
	}
	return false
}

// styled draws spans in base, with their emphasis, links and mentions.
func styled(p *Palette, v store.View, spans []mrkdwn.Span, base canvas.Style) canvas.Row {
	r := make(canvas.Row, 0, len(spans))
	for _, s := range spans {
		st := base
		if s.Mark&mrkdwn.Bold != 0 {
			st = st.With(canvas.Bold)
		}
		if s.Mark&mrkdwn.Italic != 0 {
			st = st.With(canvas.Italic)
		}
		switch s.Kind {
		case mrkdwn.Code:
			st = p.Chip
		case mrkdwn.Link:
			st = p.Blue.With(canvas.Underline)
			st.Link = s.Target
		case mrkdwn.User:
			if s.Target == v.Self() {
				st = p.Yellow.Bg(p.Ask.BG).With(canvas.Bold)
			} else {
				st = p.Blue
			}
		case mrkdwn.Channel:
			st = p.Blue
		case mrkdwn.Group, mrkdwn.Special:
			st = p.Yellow
		case mrkdwn.Emoji:
			if _, ok := emojiTable[s.Text]; !ok {
				st = p.Main.Dim
			}
		}
		if s.Mark&mrkdwn.Strike != 0 {
			st = st.With(canvas.Faint)
		}
		r = append(r, canvas.T(spanText(v, s), st))
	}
	return r
}

// lineRows draws a plain or quoted line.
func lineRows(p *Palette, v store.View, l mrkdwn.Line, w int) []canvas.Row {
	ink := p.Main
	if !l.Quote {
		return canvas.Wrap(styled(p, v, l.Spans, ink.Text), w)
	}
	bar := canvas.T("▏ ", ink.Faint)
	rows := canvas.Wrap(styled(p, v, l.Spans, ink.Sub), w-2)
	for i := range rows {
		rows[i] = append(canvas.Row{bar}, rows[i]...)
	}
	return rows
}

// listRows draws a list item: its bullet (by depth) or number, dim, and
// its text hanging under itself when it wraps.
func listRows(p *Palette, v store.View, l mrkdwn.Line, w int) []canvas.Row {
	ink := p.Main
	marker := l.Bullet
	if marker == "•" {
		marker = bullets[l.Depth%len(bullets)]
	}
	indent := 2*l.Depth + cellw.String(marker) + 1
	if indent > w/2 {
		indent = min(w/2, 2+cellw.String(marker))
	}
	rows := canvas.Wrap(styled(p, v, l.Spans, ink.Text), max(1, w-indent))
	for i := range rows {
		lead := strings.Repeat(" ", indent)
		if i == 0 {
			lead = strings.Repeat(" ", indent-cellw.String(marker)-1)
		}
		head := canvas.Row{canvas.T(lead, ink.Text)}
		if i == 0 {
			head = append(head, canvas.T(marker+" ", ink.Dim))
		}
		rows[i] = append(head, rows[i]...)
	}
	if len(rows) == 0 {
		rows = []canvas.Row{{canvas.T(strings.Repeat(" ", indent-cellw.String(marker)-1)+marker, ink.Dim)}}
	}
	return rows
}

// codeRows draws a code block on a panel the width of the message, one
// cell in from either side, highlighted if its language is known, and
// cut (not wrapped at words) where a line's too long.
func codeRows(p *Palette, lines []mrkdwn.Line, w int) []canvas.Row {
	lang := hl.For(lines[0].Lang)
	inner := max(1, w-2)
	var st hl.State
	var out []canvas.Row
	for _, l := range lines {
		src := ""
		if len(l.Spans) > 0 {
			src = strings.ReplaceAll(l.Spans[0].Text, "\t", "    ")
		}
		var r canvas.Row
		lang.Line(&st, src, func(i, j int, c hl.Class) {
			r = append(r, canvas.T(src[i:j], p.Code[c]))
		})
		for {
			out = append(out, canvas.Fit(append(canvas.Row{canvas.T(" ", p.Panel)}, canvas.Cut(r, inner)...), w, p.Panel))
			if r.Width() <= inner {
				break
			}
			r = canvas.Drop(r, inner)
		}
	}
	// The language sits at the top right, if the first line leaves room.
	if tag := lines[0].Lang; tag != "" && len(out) > 0 {
		label := canvas.Row{canvas.T(tag+" ", p.Panel.Fg(p.Main.Dim.FG))}
		at := w - label.Width()
		if used := strings.TrimRight(lines[0].Spans[0].Text, " "); cellw.String(used)+3 <= at {
			out[0] = canvas.Splice(out[0], at, label)
		}
	}
	return out
}

// tableRows lays out a table as rush does: two columns and a few rows
// read as "key · value" lines; too narrow for columns, each row is its
// cells joined by " · "; otherwise columns as wide as their widest cell,
// the widest giving up a cell at a time until it fits, cells wrapping
// within them. The head row is bold, with a faint rule under it.
func tableRows(p *Palette, v store.View, lines []mrkdwn.Line, w int) []canvas.Row {
	ink := p.Main
	head := lines[0].Head
	cols := 0
	for _, l := range lines {
		cols = max(cols, len(l.Cells))
	}
	cell := func(l mrkdwn.Line, c int, st canvas.Style) canvas.Row {
		if c >= len(l.Cells) {
			return nil
		}
		return styled(p, v, l.Cells[c], st)
	}
	dot := canvas.T(" · ", ink.Faint)
	key := ink.Sub.With(canvas.Bold)

	body := lines
	if head {
		body = lines[1:]
	}
	if cols == 2 && len(body) <= 5 {
		var out []canvas.Row
		for _, l := range body {
			r := append(cell(l, 0, key), dot)
			out = append(out, canvas.Wrap(append(r, cell(l, 1, ink.Text)...), w)...)
		}
		return out
	}
	if 3*cols-2 > w {
		var out []canvas.Row
		for i, l := range lines {
			st := ink.Text
			if i == 0 && head {
				st = key
			}
			var r canvas.Row
			for c := range cols {
				if c > 0 {
					r = append(r, dot)
				}
				r = append(r, cell(l, c, st)...)
			}
			out = append(out, canvas.Wrap(r, w)...)
		}
		return out
	}

	widths := make([]int, cols)
	for _, l := range lines {
		for c := range cols {
			widths[c] = max(widths[c], cell(l, c, ink.Text).Width())
		}
	}
	for total(widths)+2*(cols-1) > w {
		widest := 0
		for c := range widths {
			if widths[c] > widths[widest] {
				widest = c
			}
		}
		widths[widest]--
	}
	var out []canvas.Row
	for i, l := range lines {
		st := ink.Text
		if i == 0 && head {
			st = key
		}
		wrapped := make([][]canvas.Row, cols)
		h := 1
		for c := range cols {
			cst := st
			if c == 0 && !(i == 0 && head) && cols > 2 {
				cst = ink.Text.With(canvas.Bold)
			}
			wrapped[c] = canvas.Wrap(cell(l, c, cst), max(1, widths[c]))
			h = max(h, len(wrapped[c]))
		}
		for k := range h {
			var r canvas.Row
			for c := range cols {
				if c > 0 {
					r = append(r, canvas.T("  ", ink.Text))
				}
				var part canvas.Row
				if k < len(wrapped[c]) {
					part = wrapped[c][k]
				}
				r = append(r, canvas.Fit(part, widths[c], ink.Text)...)
			}
			out = append(out, r)
		}
		if i == 0 && head {
			var r canvas.Row
			for c := range cols {
				if c > 0 {
					r = append(r, canvas.T("  ", ink.Text))
				}
				r = append(r, canvas.T(strings.Repeat("─", widths[c]), ink.Faint))
			}
			out = append(out, r)
		}
	}
	return out
}

func total(xs []int) int {
	n := 0
	for _, x := range xs {
		n += x
	}
	return n
}
