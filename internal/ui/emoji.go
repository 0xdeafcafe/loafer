package ui

import (
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/emoji"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/cellw"
	"github.com/0xdeafcafe/photon/fuzzy"
)

// Emoji in the UI: shortcodes drawn as characters, and finding them by
// name, for the reaction picker (react.go) and the composer's :sm popup.
// Custom workspace emoji stay as :name: until the images lane draws them.

// emojiUI is everything emoji keeps on the model.
type emojiUI struct {
	recent []string // what you've reacted or completed with, latest first
	pick   reactor
	pop    emojiPop
}

// emojiText is a shortcode as its character, or as :name: and false when
// it isn't a standard one.
func emojiText(name string) (string, bool) {
	if c, ok := emoji.Lookup(name); ok {
		return c, true
	}
	return ":" + name + ":", false
}

// quick is what Slack offers first when it knows nothing of you.
var quick = []string{"+1", "heart", "joy", "tada", "eyes", "raised_hands", "white_check_mark", "fire", "pray", "100", "rocket", "thinking_face"}

// emojiHit is a shortcode a query found.
type emojiHit struct {
	name  string
	glyph string // "" for a custom emoji
	lit   []int  // runes of name the query matched
	score int
}

const maxRecent = 24

// used puts name first among those you reach for.
func (e *emojiUI) used(name string) {
	e.recent = slices.Insert(slices.DeleteFunc(e.recent, func(n string) bool { return n == name }), 0, name)
	e.recent = e.recent[:min(len(e.recent), maxRecent)]
}

// find is the shortcodes q matches, best first, at most limit: the ones
// you use, then the quick ones, then the rest in iamcal's order. loose
// lets letters match out of order across the name, as the jump bar does;
// else q must be inside it.
func (e *emojiUI) find(v store.View, q string, loose bool, limit int) []emojiHit {
	q = strings.TrimPrefix(strings.ToLower(q), ":")
	custom := v.CustomEmoji()
	hit := func(n string) emojiHit {
		g, _ := emoji.Lookup(n)
		return emojiHit{name: n, glyph: g}
	}
	var out []emojiHit
	if q == "" {
		seen := map[string]bool{}
		for _, ns := range [][]string{e.recent, quick, emoji.Names(), custom} {
			for _, n := range ns {
				if len(out) >= limit {
					return out
				}
				if _, ok := emoji.Lookup(n); !ok && !slices.Contains(custom, n) {
					continue // a custom emoji that's gone
				}
				if !seen[n] {
					seen[n] = true
					out = append(out, hit(n))
				}
			}
		}
		return out
	}
	for _, ns := range [][]string{emoji.Names(), custom} {
		for _, n := range ns {
			score, lit, ok := fuzzy.Match(q, n)
			if !ok || (!loose && score < 100) {
				continue
			}
			if i := slices.Index(e.recent, n); i >= 0 {
				score += 40 - i
			} else if i := slices.Index(quick, n); i >= 0 {
				score += 15 - i
			}
			if n == q {
				score += 50
			}
			h := hit(n)
			h.lit, h.score = lit, score-len(n)/3 // shorter wins a tie
			out = append(out, h)
		}
	}
	slices.SortStableFunc(out, func(a, b emojiHit) int { return b.score - a.score })
	return out[:min(len(out), limit)]
}

// --- the :sm popup ---

// emojiPop completes a shortcode being typed in the composer.
type emojiPop struct {
	on         bool
	start      int // where the ':' is in the input
	cur, n     int // where the cursor was, and how long the input, when hits were found
	query      string
	at         int
	hits       []emojiHit
	quiet      string // esc closed the popup for this query
	quietStart int
}

const popRows = 6

func isNameRune(r rune) bool {
	return r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '+' || r == '-' || r == '\'')
}

// emojiComplete looks at what's before the cursor and opens, moves or
// closes the popup to suit. It's cheap when nothing's changed.
func (m *Model) emojiComplete(v store.View) {
	p := &m.emo.pop
	start := -1
	if m.focus == onCompose && m.cur <= len(m.input) {
		i := m.cur
		for i > 0 && isNameRune(m.input[i-1]) {
			i--
		}
		// A colon at the start of a word, and two letters after it: not "10:30" or ":)".
		if i > 0 && m.cur-i >= 2 && m.input[i-1] == ':' && (i == 1 || unicode.IsSpace(m.input[i-2])) {
			start = i - 1
		}
	}
	if start < 0 {
		*p = emojiPop{}
		return
	}
	q := string(m.input[start+1 : m.cur])
	switch {
	case p.on && p.start == start && p.query == q:
	case p.quiet == q && p.quietStart == start:
		p.on = false
	default:
		p.hits = m.emo.find(v, q, false, popRows)
		p.on, p.start, p.query, p.at = len(p.hits) > 0, start, q, 0
	}
	p.cur, p.n = m.cur, len(m.input)
}

// emojiKey takes the keys the popup and the picker want before anything
// else does; ok is false for the rest.
func (m *Model) emojiKey(k tea.KeyPressMsg, s string) (cmd tea.Cmd, ok bool) {
	if m.emo.pick.on && s != "ctrl+c" && s != "f12" {
		return m.reactKey(k), true
	}
	if m.focus != onCompose {
		return nil, false
	}
	m.st.Read(m.emojiComplete)
	p := &m.emo.pop
	if !p.on {
		return nil, false
	}
	switch s {
	case "tab", "enter":
		h := p.hits[p.at]
		text := []rune(":" + h.name + ": ")
		m.input = append(m.input[:p.start], append(text, m.input[m.cur:]...)...)
		m.cur = p.start + len(text)
		m.emo.used(h.name)
		*p = emojiPop{}
	case "down":
		p.at = (p.at + 1) % len(p.hits)
	case "up":
		p.at = (p.at + len(p.hits) - 1) % len(p.hits)
	case "esc":
		p.quiet, p.quietStart, p.on = p.query, p.start, false
	default:
		return nil, false
	}
	return nil, true
}

// popEmoji draws the popup over the bottom of the message list, just
// above the composer.
func (m *Model) popEmoji(v store.View, list []canvas.Row, w int) []canvas.Row {
	m.emojiComplete(v)
	p := &m.emo.pop
	if !p.on || len(list) < len(p.hits) || w < 20 {
		return list
	}
	ink := m.pal.Main
	fill := ink.Text.Bg(ink.Sel.BG)
	bw := 0
	for _, h := range p.hits {
		bw = max(bw, len(h.name)+8)
	}
	bw = min(bw, w-4)
	for i, h := range p.hits {
		base := fill
		mark := canvas.T("  ", base)
		if i == p.at {
			base = fill.Bg(ink.Hover.BG)
			mark = canvas.T("▍ ", base.Fg(m.pal.Orange.FG))
		}
		glyph := h.glyph
		if glyph == "" {
			glyph = "◌" // a custom emoji, which has no character
		}
		row := canvas.Row{mark, canvas.T(glyph, base), canvas.T(strings.Repeat(" ", max(1, 3-cellw.String(glyph))), base)}
		for j, r := range []rune(h.name) {
			st := base
			if slices.Contains(h.lit, j) {
				st = base.Fg(m.pal.Orange.FG).With(canvas.Bold)
			}
			row = append(row, canvas.T(string(r), st)) // a seg a rune; names are short
		}
		at := len(list) - len(p.hits) + i
		list[at] = canvas.Splice(list[at], 3, canvas.Fit(row, bw, base))
	}
	return list
}
