package ui

import (
	"math"

	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/theme"
)

// The bands loafer (docs/ui.md, Logo; tools/logo variant 1), in half
// blocks, and its lace animation: the sole runs heel to toe, the upper
// sweeps in after it, then the strap and the penny. Its frames are made
// once per ground.

var bandsArt = []string{
	"..................",
	"......UUU.........",
	".UU..UUUUUU.......",
	"UUUUSSSPPSSUUU....",
	"UUUUUUUUUUUUUUUU..",
	"UUUUUUUUUUUUUUUUUU",
	"OOOOOOOOOOOOOOOOOO",
	"HHHH..............",
}

var (
	slackBrand = []theme.RGB{rgb(0xE0, 0x1E, 0x5A), rgb(0xEC, 0xB2, 0x2E), rgb(0x2E, 0xB6, 0x7D), rgb(0x36, 0xC5, 0xF0)}
	white      = rgb(255, 255, 255)
)

// standOut pushes c away from bg until it stands out by want, as the
// logo tool does: lighter on a dark ground, darker on a light one.
func standOut(c, bg theme.RGB, want float64) theme.RGB {
	end := white
	if !bg.Dark() {
		end = theme.RGB{}
	}
	out := c
	for t := .05; theme.Contrast(out, bg) < want && t <= 1; t += .05 {
		out = theme.Mix(c, end, t)
	}
	return out
}

// logoFrame is lace's frame f, or the still logo from laceN on.
func (m *Model) logoFrame(f int) []canvas.Row {
	bg := m.pal.Side.Ground
	if m.wel.logo == nil || m.wel.logoBG != bg {
		m.wel.logoBG, m.wel.logo = bg, make([][]canvas.Row, laceN+1)
		for i := range m.wel.logo {
			m.wel.logo[i] = bandsFrame(bg, i)
		}
	}
	return m.wel.logo[min(f, laceN)]
}

// bandsFrame draws the logo on bg, lace's frame f (laceN: all of it).
func bandsFrame(bg theme.RGB, f int) []canvas.Row {
	aw, ah := len(bandsArt[0]), len(bandsArt)
	sole := standOut(rgb(0x4A, 0x15, 0x4B), bg, 2)
	t := float64(f) / float64(laceN-5)
	px := func(x, y int) (theme.RGB, bool) {
		b := bandsArt[y][x]
		if b == '.' {
			return theme.RGB{}, false
		}
		fx := (float64(x) + .5) / float64(aw)
		band := standOut(slackBrand[int(math.Floor(fx*4))], bg, 3)
		c := band
		switch b {
		case 'S':
			c = theme.Mix(band, theme.RGB{}, .45)
		case 'P':
			c = rgb(250, 244, 228)
		case 'O', 'H':
			c = sole
		}
		if f >= laceN {
			return c, true
		}
		x01, y01 := float64(x)/float64(aw-1), float64(y)/float64(ah)
		switch b {
		case 'O', 'H':
			return c, x01 <= t*2.2
		case 'S':
			return c, f >= laceN-6
		case 'P':
			return c, f >= laceN-4
		}
		e := t*1.5 - .3 - x01 - (1-y01)*.25
		if e >= 0 && e < .12 {
			c = theme.Mix(c, white, .55)
		}
		return c, e >= 0
	}
	ground := canvas.Style{}.Bg(bg)
	out := make([]canvas.Row, ah/2)
	for cy := range out {
		row := canvas.Row{canvas.T(" ", ground)}
		for x := range aw {
			top, tok := px(x, 2*cy)
			bot, bok := px(x, 2*cy+1)
			switch {
			case !tok && !bok:
				row = append(row, canvas.T(" ", ground))
			case tok && bok && top == bot:
				row = append(row, canvas.T("█", ground.Fg(top)))
			case tok && bok:
				row = append(row, canvas.T("▀", canvas.Style{}.Bg(bot).Fg(top)))
			case tok:
				row = append(row, canvas.T("▀", ground.Fg(top)))
			default:
				row = append(row, canvas.T("▄", ground.Fg(bot)))
			}
		}
		out[cy] = append(row, canvas.T(" ", ground))
	}
	return out
}
