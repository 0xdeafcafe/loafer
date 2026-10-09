// Package emoji is Slack's standard shortcodes as characters: the
// iamcal/emoji-data names, aliases such as +1 and thumbsup, and skin tones
// as Slack writes them (thumbsup::skin-tone-3). Custom workspace emoji
// aren't here; the store has those.
package emoji

import (
	"slices"
	"strconv"
	"strings"
	"sync"
)

//go:generate go run ../../tools/emoji

var (
	once   sync.Once
	chars  map[string]string     // every name, to its character
	tones  map[string]*[5]string // a name, to its skin-tone-2 to 6 forms, where it has them
	names  []string              // every name, in iamcal's order (related ones together)
	canon  map[string]string     // an alias, to the name Slack files it under
	groups []Group               // iamcal's categories, skin tone swatches left out
)

// Group is a category of emoji, in Unicode's order: its name and the
// first name of each emoji in it.
type Group struct {
	Name  string
	Names []string
}

// build reads the table on first use, so starting up doesn't pay for it.
func build() {
	chars = make(map[string]string, 3000)
	tones = make(map[string]*[5]string, 1000)
	canon = make(map[string]string, 800)
	for line := range strings.Lines(data) {
		if g, ok := strings.CutPrefix(line, "#"); ok {
			groups = append(groups, Group{Name: strings.TrimSuffix(g, "\n")})
			continue
		}
		f := strings.Split(strings.TrimSuffix(line, "\n"), "\t")
		var skins *[5]string
		if len(f) == 7 {
			skins = (*[5]string)(f[2:])
		}
		first, _, _ := strings.Cut(f[0], ",")
		if g := &groups[len(groups)-1]; g.Name != "Component" {
			g.Names = append(g.Names, first)
		}
		for n := range strings.SplitSeq(f[0], ",") {
			chars[n] = f[1]
			if n != first {
				canon[n] = first
			}
			if skins != nil {
				tones[n] = skins
			}
			names = append(names, n)
		}
	}
	groups = slices.DeleteFunc(groups, func(g Group) bool { return len(g.Names) == 0 })
}

// Lookup is the character for a shortcode, given without its colons.
func Lookup(name string) (string, bool) {
	once.Do(build)
	if c, ok := chars[name]; ok {
		return c, true
	}
	base, tone, ok := strings.Cut(name, "::skin-tone-")
	if n, err := strconv.Atoi(tone); ok && err == nil && n >= 2 && n <= 6 {
		if t := tones[base]; t != nil {
			return t[n-2], true
		}
	}
	return "", false
}

// Canon is the name Slack files a reaction under: thumbsup is +1. Names
// that aren't aliases come back as they are.
func Canon(name string) string {
	once.Do(build)
	base, tone, hasTone := strings.Cut(name, "::")
	if c, ok := canon[base]; ok {
		base = c
	}
	if hasTone {
		return base + "::" + tone
	}
	return base
}

// Names is every shortcode, skin tones apart, in a useful order. Don't
// change it.
func Names() []string {
	once.Do(build)
	return names
}

// Groups is the emoji by category, in Unicode's order: Smileys & Emotion,
// People & Body, Animals & Nature, Food & Drink, Travel & Places,
// Activities, Objects, Symbols, Flags. Don't change it.
func Groups() []Group {
	once.Do(build)
	return groups
}
