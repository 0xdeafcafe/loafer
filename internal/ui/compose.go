package ui

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/0xdeafcafe/loafer/internal/mrkdwn"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
)

// The composer's text is m.input, readable as typed ("@Alex"). A
// mention is a run of it that stands for something Slack wants spelt
// otherwise (<@U1>): m.ments keeps those runs, sorted and apart, and
// every edit goes through splice so they move with the text.

type mention struct {
	from, to int    // runes of the input
	code     string // what Slack is sent: <@U1>, <#C1>, <!here>, a link as it came
}

// draft is a composer's contents, kept while you're elsewhere.
type draft struct {
	text  []rune
	ments []mention
}

func (m *Model) keep(id string) { m.drafts[id] = draft{m.input, m.ments} }

func (m *Model) restore(id string) {
	d := m.drafts[id]
	delete(m.drafts, id)
	m.load(d.text, d.ments)
}

// load puts text in the box, cursor at the end.
func (m *Model) load(text []rune, ments []mention) {
	m.input, m.ments, m.cur, m.pop = text, ments, len(text), popup{}
}

// splice replaces input[from:to] with r and leaves the cursor after it.
// Mentions after it move; one the edit lands inside turns to plain text,
// and a deletion that touches one takes all of it.
func (m *Model) splice(from, to int, r []rune) {
	if len(r) == 0 {
		for _, mn := range m.ments {
			if mn.from < to && mn.to > from {
				from, to = min(from, mn.from), max(to, mn.to)
			}
		}
	}
	m.input = slices.Replace(m.input, from, to, r...)
	d := len(r) - (to - from)
	keep := m.ments[:0]
	for _, mn := range m.ments {
		switch {
		case mn.to <= from:
		case mn.from >= to:
			mn.from, mn.to = mn.from+d, mn.to+d
		default:
			continue
		}
		keep = append(keep, mn)
	}
	m.ments, m.cur = keep, from+len(r)
}

var escape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// encode is the text as Slack takes it: mentions as their codes, the
// rest escaped. It undoes decode.
func encode(in []rune, ments []mention) string {
	var b strings.Builder
	at := 0
	for _, mn := range ments {
		b.WriteString(escape.Replace(string(in[at:mn.from])))
		b.WriteString(mn.code)
		at = mn.to
	}
	b.WriteString(escape.Replace(string(in[at:])))
	return b.String()
}

// decode reads Slack's text for the box. Every <…> becomes a mention
// that keeps its code, so what isn't understood (a link, a user group)
// goes back as it came.
func decode(v store.View, s string) ([]rune, []mention) {
	var out []rune
	var ments []mention
	for s != "" {
		i := strings.IndexByte(s, '<')
		end := -1
		if i >= 0 {
			end = strings.IndexByte(s[i:], '>')
		}
		if end < 0 {
			out = append(out, []rune(mrkdwn.Unescape(s))...)
			break
		}
		out = append(out, []rune(mrkdwn.Unescape(s[:i]))...)
		code := s[i : i+end+1]
		label := tokenLabel(v, code[1:len(code)-1])
		ments = append(ments, mention{len(out), len(out) + utf8.RuneCountInString(label), code})
		out = append(out, []rune(label)...)
		s = s[i+end+1:]
	}
	return out, ments
}

// tokenLabel is how the inside of a <…> reads in the box.
func tokenLabel(v store.View, body string) string {
	ref, label, _ := strings.Cut(body, "|")
	switch {
	case strings.HasPrefix(ref, "@"):
		name := v.Person(ref[1:]).Name
		if name == ref[1:] && label != "" {
			name = label // users.list hasn't said
		}
		return "@" + name
	case strings.HasPrefix(ref, "#"):
		name := label
		if c := v.Conv(ref[1:]); c != nil {
			name = c.Name
		}
		if name == "" {
			return ref
		}
		return "#" + strings.TrimPrefix(name, "#")
	case strings.HasPrefix(ref, "!subteam^") && label == "":
		id := strings.TrimPrefix(ref, "!subteam^")
		if g, ok := v.Group(id); ok {
			return "@" + g.Handle
		}
		return "@" + id
	case strings.HasPrefix(ref, "!") && label == "":
		return "@" + strings.TrimPrefix(strings.SplitN(ref[1:], "^", 2)[0], "@")
	}
	if label != "" {
		return label
	}
	return ref
}

// --- moving about ---

func (m *Model) lineStart(i int) int {
	for i > 0 && m.input[i-1] != '\n' {
		i--
	}
	return i
}

func (m *Model) lineEnd(i int) int {
	for i < len(m.input) && m.input[i] != '\n' {
		i++
	}
	return i
}

func blank(r rune) bool { return r == ' ' || r == '\n' }

func (m *Model) wordLeft(i int) int {
	for i > 0 && blank(m.input[i-1]) {
		i--
	}
	for i > 0 && !blank(m.input[i-1]) {
		i--
	}
	return i
}

func (m *Model) wordRight(i int) int {
	for i < len(m.input) && blank(m.input[i]) {
		i++
	}
	for i < len(m.input) && !blank(m.input[i]) {
		i++
	}
	return i
}

// vertical moves the cursor d lines, keeping its column where the line
// is long enough. It reports whether there was a line to go to.
func (m *Model) vertical(d int) bool {
	start := m.lineStart(m.cur)
	col := m.cur - start
	if d < 0 {
		if start == 0 {
			return false
		}
		prev := m.lineStart(start - 1)
		m.cur = min(prev+col, start-1)
		return true
	}
	end := m.lineEnd(m.cur)
	if end == len(m.input) {
		return false
	}
	m.cur = min(end+1+col, m.lineEnd(end+1))
	return true
}

// --- drawing ---

// inputRow is the box's text: mentions in their own style, and a block
// where the cursor is if caret is set.
func (m *Model) inputRow(field, link, caret canvas.Style, focused bool) canvas.Row {
	var row canvas.Row
	var b strings.Builder
	cur := field
	put := func(st canvas.Style, s string) {
		if st != cur && b.Len() > 0 {
			row = append(row, canvas.T(b.String(), cur))
			b.Reset()
		}
		cur = st
		b.WriteString(s)
	}
	mi := 0
	for i, r := range m.input {
		for mi < len(m.ments) && m.ments[mi].to <= i {
			mi++
		}
		st := field
		if mi < len(m.ments) && m.ments[mi].from <= i {
			st = link
		}
		switch {
		case !focused || i != m.cur:
			put(st, string(r))
		case r == '\n':
			put(caret, " ")
			put(st, "\n")
		default:
			put(caret, string(r))
		}
	}
	if focused && m.cur == len(m.input) {
		put(caret, " ")
	}
	if b.Len() > 0 {
		row = append(row, canvas.T(b.String(), cur))
	}
	return row
}

// inView is the most lines of the box that fit: the end of it, unless
// the cursor is further up.
func inView(lines []canvas.Row, most int, caret canvas.Style) []canvas.Row {
	if len(lines) <= most {
		return lines
	}
	at := len(lines) - 1
	for i, l := range lines {
		if slices.ContainsFunc(l, func(s canvas.Seg) bool { return s.St == caret }) {
			at = i
			break
		}
	}
	from := min(at, len(lines)-most)
	return lines[from : from+most]
}
