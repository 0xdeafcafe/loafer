package ui

import (
	"slices"
	"strings"

	"github.com/0xdeafcafe/loafer/internal/emoji"
	"github.com/0xdeafcafe/loafer/internal/store"
)

// Emoji in the UI: shortcodes drawn as characters, and finding them by
// name, for the reaction picker (react.go) and the composer's :sm
// completion, which is a trigger of complete.go's popup. Custom workspace
// emoji are pictures where the terminal draws them (picture.go), else
// :name: in dim.

// emojiUI is everything emoji keeps on the model.
type emojiUI struct {
	recent []string // what you've reacted with, latest first
	pick   reactor
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
			score, lit, ok := match(q, n)
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

// shortcodes is the popup's offer for a :query, from two letters on (not
// ":)" or "10:30"). It puts the character in the detail and the :name: in
// the box.
func (m *Model) shortcodes(v store.View, q string, out []item) []item {
	if len(q) < 2 {
		return out
	}
	for _, h := range m.emo.find(v, q, false, popCap) {
		it := item{label: ":" + h.name + ":", detail: h.glyph, score: h.score}
		for _, i := range h.lit {
			it.lit = append(it.lit, i+1)
		}
		out = append(out, it)
	}
	return out
}
