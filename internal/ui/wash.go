package ui

import (
	"hash/fnv"
	"slices"
	"strconv"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/theme"
)

// The header's wash (docs/ui.md, rule 2a): rush's static gradient, plain
// for the first 60% of the width, then ramping in four-cell steps into
// the workspace's own colour at the right edge. The sidebar keeps the
// aubergine; the wash is what tells one workspace from another.

// wsColours are what a workspace without a theme of its own gets, by a
// hash of its id: Slack's theme colours and a few more like them, all
// dark enough for the header's text.
var wsColours = []theme.RGB{
	rgb(0x0d, 0x7e, 0x83), rgb(0x30, 0x3e, 0x4d), rgb(0x54, 0x45, 0x38), rgb(0x1f, 0x5c, 0x45),
	rgb(0x7a, 0x2e, 0x1f), rgb(0x1d, 0x3b, 0x6e), rgb(0x6b, 0x4a, 0x0e), rgb(0x5a, 0x2a, 0x6e),
}

// teamColour is t's colour: its sidebar theme's when Slack gave one, else
// one of wsColours that stays the same for it.
func teamColour(t slack.Team) theme.RGB {
	if len(t.Colour) == 7 {
		if v, err := strconv.ParseUint(t.Colour[1:], 16, 32); err == nil {
			return rgb(uint8(v>>16), uint8(v>>8), uint8(v))
		}
	}
	h := fnv.New32a()
	h.Write([]byte(t.ID))
	return wsColours[h.Sum32()%uint32(len(wsColours))]
}

// washCache is the wash's ground cell by cell, made again only when the
// width, the ground or the team changes, and the header rows last washed,
// so a frame whose header hasn't changed washes nothing.
type washCache struct {
	w       int
	from    theme.RGB
	team    slack.Team
	cols    []theme.RGB
	in, out [2]canvas.Row
}

// washed is the header's two rows (no more) with the wash behind them: wherever a
// row is on the sidebar's ground, it takes the wash's colour instead.
func (m *Model) washed(team slack.Team, rows ...canvas.Row) []canvas.Row {
	ink := m.pal.Side
	c := &m.wash
	if c.w != m.w || c.from != ink.Ground || c.team != team {
		*c = washCache{w: m.w, from: ink.Ground, team: team, cols: washColours(ink, teamColour(team), m.w)}
	}
	for k, r := range rows {
		if c.out[k] != nil && slices.Equal(c.in[k], r) {
			continue
		}
		var out canvas.Row
		for a := 0; a < len(c.cols); {
			b := a + 1
			for b < len(c.cols) && c.cols[b] == c.cols[a] {
				b++
			}
			part := canvas.Cut(canvas.Drop(r, a), b-a) // Cut makes a new row, so this restyles nothing of r's
			for i := range part {
				if part[i].St.BG == ink.Ground {
					part[i].St = part[i].St.Bg(c.cols[a])
				}
			}
			out = append(out, part...)
			a = b
		}
		c.in[k], c.out[k] = r, canvas.Fit(out, m.w, ink.Text)
	}
	return c.out[:len(rows)]
}

// washColours ramps from ink's ground to to across the last 40% of w,
// held back wherever the header's sub and dim text would stop reading.
func washColours(ink Inks, to theme.RGB, w int) []theme.RGB {
	cols := make([]theme.RGB, w)
	start := (w*6 + 9) / 10
	steps := max(1, (w-start+3)/4-1)
	// As readable as on the ground itself, up to AA for sub and 3:1 for dim.
	subAt := min(4.5, theme.Contrast(ink.Sub.FG, ink.Ground))
	dimAt := min(3, theme.Contrast(ink.Dim.FG, ink.Ground))
	for i := range cols {
		if i < start {
			cols[i] = ink.Ground
			continue
		}
		if (i-start)%4 != 0 {
			cols[i] = cols[i-1]
			continue
		}
		u := min(1, float64((i-start)/4)/float64(steps))
		c := theme.Mix(ink.Ground, to, u*u*(3-2*u))
		for range 24 {
			if theme.Contrast(ink.Sub.FG, c) >= subAt && theme.Contrast(ink.Dim.FG, c) >= dimAt {
				break
			}
			c = theme.Mix(c, ink.Ground, .12)
		}
		cols[i] = c
	}
	return cols
}

// railInk is text that reads on c: the header's on a dark colour, dark
// text on a light one.
func railInk(c theme.RGB) theme.RGB {
	if c.Dark() {
		return rgb(240, 236, 228)
	}
	return theme.Light.FG
}
