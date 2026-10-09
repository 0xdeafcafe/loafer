// Package mrkdwn reads Slack's message markup into spans: text with
// emphasis, code, links, mentions and emoji names, line by line, so the
// renderer can style and wrap them without knowing Slack's syntax.
//
// Slack's rules, as its clients draw them: <...> is a link or mention,
// *bold* _italic_ ~strike~ `code` only open after a space or punctuation
// and close before one, ``` fences a block, > starts a quote, and &amp;
// &lt; &gt; are the only entities.
//
// Bots and people pasting from elsewhere write markdown, so that's read
// too: # headings, - and 1. lists, | tables |, --- rules, a language
// after ```, **bold**, ~~strike~~, [links](https://…) and bare URLs.
// Where the two disagree Slack wins: *this* is bold, as Slack shows it.
package mrkdwn

import (
	"strings"
)

type Kind uint8

const (
	Text    Kind = iota
	Code         // inline `code`
	Link         // URL in Target, label in Text
	User         // <@U123>: ID in Target, Text is any label Slack gave
	Channel      // <#C123|name>
	Group        // <!subteam^S123|@team>
	Special      // <!here>, <!channel>, <!everyone>, <!date…>: Text is what to show
	Emoji        // :name: in Text, without the colons
)

// Mark is emphasis, which can stack.
type Mark uint8

const (
	Bold Mark = 1 << iota
	Italic
	Strike
)

type Span struct {
	Kind   Kind
	Mark   Mark
	Text   string
	Target string
}

// Line is one line of a message, and what sort of line it is.
type Line struct {
	Spans []Span

	Pre   bool   // inside a ``` block: draw as code, don't wrap at words
	Start bool   // the first line of a ``` block
	Lang  string // the language named after the opening ```, if one was

	Quote   bool     // a > line
	Heading int      // 1 to 6 for a # heading
	Rule    bool     // a --- line
	Bullet  string   // a list item's marker: "•" for - * + •, else "1." as written
	Depth   int      // a list item's nesting, from 0
	Cells   [][]Span // a | table | row
	Head    bool     // the table row above the |---| one
}

// Parse reads a message's text into lines.
func Parse(text string) []Line {
	var out []Line
	pre, start, lang := false, false, ""
	var indents []int // the open list items' indents, outermost first
	for raw := range strings.SplitSeq(text, "\n") {
		// ``` can open and close mid-line; split the line on fences.
		parts := strings.Split(raw, "```")
		for i, part := range parts {
			if i > 0 {
				pre, start, lang = !pre, !pre, ""
				if part == "" && i == len(parts)-1 {
					break // a fence at the end of the line
				}
				// ```go on a line of its own names the language.
				if pre && i == 1 && len(parts) == 2 && parts[0] == "" && langTag(part) {
					lang = part
					break
				}
			}
			if part == "" && i == 0 && len(parts) > 1 {
				continue // a fence at the start of the line
			}
			if pre {
				out = append(out, Line{Spans: []Span{{Kind: Code, Text: Unescape(part)}}, Pre: true, Start: start, Lang: lang})
				start = false
				continue
			}
			if len(parts) == 1 {
				var prev *Line
				if len(out) > 0 {
					prev = &out[len(out)-1]
				}
				if l, ok, keep := block(part, &indents, prev); ok {
					if keep {
						out = append(out, l)
					}
					continue
				}
			}
			indents = indents[:0]
			l := Line{}
			if q, ok := strings.CutPrefix(part, "&gt;"); ok && i == 0 {
				l.Quote, part = true, strings.TrimPrefix(q, " ")
			}
			l.Spans = inline(part)
			out = append(out, l)
		}
	}
	// A fence that wasn't closed: Slack shows the ``` as typed, so the
	// language name is all that would be lost. Close enough.
	return out
}

// langTag says whether what follows an opening ``` reads as a language
// name rather than code: one short word.
func langTag(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("+#.-_", c) >= 0) {
			return false
		}
	}
	return true
}

// block reads s as a markdown block line (heading, rule, table row or
// list item) if it is one. ok says it was; keep that l is a line of its
// own (a table's |---| row isn't: it marks prev as the table's head).
func block(s string, indents *[]int, prev *Line) (l Line, ok, keep bool) {
	t := strings.TrimSpace(s)
	switch {
	case t == "":
		return l, false, false
	case t[0] == '#':
		n := 0
		for n < len(t) && t[n] == '#' {
			n++
		}
		if n <= 6 && n < len(t) && t[n] == ' ' {
			*indents = (*indents)[:0]
			return Line{Heading: n, Spans: inline(strings.TrimSpace(t[n:]))}, true, true
		}
	case rule(t):
		*indents = (*indents)[:0]
		return Line{Rule: true}, true, true
	case t[0] == '|' && len(t) > 1:
		*indents = (*indents)[:0]
		cells := strings.Split(strings.TrimSuffix(t[1:], "|"), "|")
		if prev != nil && prev.Cells != nil && separator(cells) {
			prev.Head = true
			return l, true, false
		}
		for _, c := range cells {
			l.Cells = append(l.Cells, inline(strings.TrimSpace(c)))
		}
		return l, true, true
	}
	if bullet, rest, ok := listItem(t); ok {
		col := 0
		for _, c := range s[:len(s)-len(strings.TrimLeft(s, " \t"))] {
			if c == '\t' {
				col += 4
			} else {
				col++
			}
		}
		st := *indents
		for len(st) > 0 && st[len(st)-1] > col {
			st = st[:len(st)-1]
		}
		if len(st) == 0 || st[len(st)-1] < col {
			st = append(st, col)
		}
		*indents = st
		return Line{Bullet: bullet, Depth: len(st) - 1, Spans: inline(rest)}, true, true
	}
	return l, false, false
}

// rule is ---, *** or ___, at least three, spaces allowed between.
func rule(t string) bool {
	if len(t) < 3 || strings.IndexByte("-*_", t[0]) < 0 {
		return false
	}
	n := 0
	for i := 0; i < len(t); i++ {
		switch t[i] {
		case t[0]:
			n++
		case ' ':
		default:
			return false
		}
	}
	return n >= 3
}

// separator is a table's |---|:--:| row.
func separator(cells []string) bool {
	for _, c := range cells {
		c = strings.Trim(strings.TrimSpace(c), ":")
		if len(c) < 1 || strings.Trim(c, "-") != "" {
			return false
		}
	}
	return true
}

// listItem reads a list item's marker: - * + • ◦ ▪ then a space are a
// bullet, and 1. or 1) a number, kept as written.
func listItem(t string) (bullet, rest string, ok bool) {
	for _, b := range []string{"- ", "* ", "+ ", "• ", "◦ ", "▪ "} {
		if r, ok := strings.CutPrefix(t, b); ok {
			return "•", r, true
		}
	}
	n := 0
	for n < len(t) && n < 3 && t[n] >= '0' && t[n] <= '9' {
		n++
	}
	if n > 0 && n+1 < len(t) && (t[n] == '.' || t[n] == ')') && t[n+1] == ' ' {
		return t[:n+1], t[n+2:], true
	}
	return "", "", false
}

var entities = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">")

// Unescape turns Slack's three entities back into what they stand for.
func Unescape(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	return entities.Replace(s)
}

// inline reads one line's spans.
func inline(s string) []Span {
	var out []Span
	var mark Mark
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, Span{Kind: Text, Mark: mark, Text: Unescape(buf.String())})
			buf.Reset()
		}
	}
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '<':
			end := strings.IndexByte(s[i:], '>')
			if end < 0 {
				break
			}
			flush()
			sp := angle(s[i+1 : i+end])
			sp.Mark = mark
			out = append(out, sp)
			i += end + 1
			continue
		case c == '`':
			end := strings.IndexByte(s[i+1:], '`')
			if end < 0 {
				break
			}
			flush()
			out = append(out, Span{Kind: Code, Text: Unescape(s[i+1 : i+1+end])})
			i += end + 2
			continue
		case c == ':':
			if end := strings.IndexByte(s[i+1:], ':'); end > 0 && emojiName(s[i+1:i+1+end]) {
				flush()
				// :+1::skin-tone-3: is one emoji.
				if tone, ok := strings.CutPrefix(s[i+end+2:], ":skin-tone-"); ok && len(tone) > 1 && tone[0] >= '2' && tone[0] <= '6' && tone[1] == ':' {
					end += len(":skin-tone-N:")
				}
				out = append(out, Span{Kind: Emoji, Mark: mark, Text: s[i+1 : i+1+end]})
				i += end + 2
				continue
			}
		case c == '[':
			// [label](https://…), markdown's link.
			if mid := strings.Index(s[i:], "]("); mid > 0 {
				if end := strings.IndexByte(s[i+mid:], ')'); end > 0 {
					if target := Unescape(s[i+mid+2 : i+mid+end]); web(target) {
						flush()
						out = append(out, Span{Kind: Link, Mark: mark, Text: Unescape(s[i+1 : i+mid]), Target: target})
						i += mid + end + 1
						continue
					}
				}
			}
		case c == 'h' && boundary(s, i-1) && (strings.HasPrefix(s[i:], "https://") || strings.HasPrefix(s[i:], "http://")):
			// A bare URL, up to a space, less any punctuation ending the sentence.
			end := strings.IndexAny(s[i:], " \t<>")
			if end < 0 {
				end = len(s) - i
			}
			target := strings.TrimRight(s[i:i+end], ".,;:!?)'\"")
			flush()
			out = append(out, Span{Kind: Link, Mark: mark, Text: Unescape(target), Target: Unescape(target)})
			i += len(target)
			continue
		case (c == '*' || c == '_' || c == '~') && i+1 < len(s) && s[i+1] == c:
			// **bold**, __bold__ and ~~strike~~, markdown's doubled marks.
			m := markOf(c, true)
			opens := mark&m == 0 && i+2 < len(s) && s[i+2] != ' ' && strings.Contains(s[i+2:], s[i:i+2])
			closes := mark&m != 0 && i > 0 && s[i-1] != ' '
			if opens || closes {
				flush()
				mark ^= m
				i += 2
				continue
			}
		case c == '*' || c == '_' || c == '~':
			m := markOf(c, false)
			opens := mark&m == 0 && boundary(s, i-1) && i+1 < len(s) && s[i+1] != ' ' && strings.IndexByte(s[i+1:], c) > 0
			closes := mark&m != 0 && i > 0 && s[i-1] != ' ' && boundary(s, i+1)
			if opens || closes {
				flush()
				mark ^= m
				i++
				continue
			}
		}
		buf.WriteByte(c)
		i++
	}
	flush()
	return out
}

// boundary says whether s[i] is the edge of a word (or past either end).
func boundary(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	return strings.IndexByte(" \t.,;:!?()[]{}\"'-*_~", s[i]) >= 0
}

func emojiName(n string) bool {
	if len(n) == 0 || len(n) > 64 {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '+' || c == '\'') {
			return false
		}
	}
	return true
}

// angle reads what was between < and >.
func angle(in string) Span {
	target, label, _ := strings.Cut(in, "|")
	label = Unescape(label)
	switch {
	case strings.HasPrefix(target, "@"):
		return Span{Kind: User, Target: target[1:], Text: label}
	case strings.HasPrefix(target, "#"):
		return Span{Kind: Channel, Target: target[1:], Text: label}
	case strings.HasPrefix(target, "!subteam^"):
		return Span{Kind: Group, Target: strings.TrimPrefix(target, "!subteam^"), Text: label}
	case strings.HasPrefix(target, "!"):
		name := target[1:]
		if label != "" {
			return Span{Kind: Special, Target: name, Text: label}
		}
		if k, _, _ := strings.Cut(name, "^"); k == "here" || k == "channel" || k == "everyone" {
			return Span{Kind: Special, Target: k, Text: "@" + k}
		}
		return Span{Kind: Special, Target: name, Text: name}
	}
	target = Unescape(target)
	if label == "" {
		label = strings.TrimPrefix(strings.TrimPrefix(target, "mailto:"), "tel:")
	}
	return Span{Kind: Link, Target: target, Text: label}
}

// web says whether a markdown link goes somewhere worth linking to.
func web(u string) bool {
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "mailto:")
}

// markOf is the emphasis c stands for; doubled, __ is bold as ** is.
func markOf(c byte, doubled bool) Mark {
	switch {
	case c == '*', c == '_' && doubled:
		return Bold
	case c == '_':
		return Italic
	}
	return Strike
}
