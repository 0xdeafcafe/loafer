package ui

import (
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/theme"
)

// Inks is one ground's text colours, as styles on that ground.
type Inks struct {
	Ground                              theme.RGB
	Text, Sub, Dim, Faint, Bright, Edge canvas.Style
	Sel, Hover                          canvas.Style // row fills, text in Text
}

// Palette is rush's colours, each written as it is on the dark ground and
// moved onto the terminal's (docs/ui.md, Rules). Main is the message
// panes' ground; Side is the workspace colour the header, tabs and
// sidebar sit on.
type Palette struct {
	Main, Side                                 Inks
	Orange, Yellow, Blue, Green, Red, Lavender canvas.Style    // accents on Main
	NewRule                                    canvas.Style    // the "new" line's rule: orange, half toward the ground
	SideOrange, SideYellow, SideGreen          canvas.Style    // accents on Side
	Panel, Input, Chip, Ask, Err               canvas.Style    // surfaces on Main
	Code                                       [7]canvas.Style // on Panel, by hl.Class
}

func rgb(r, g, b uint8) theme.RGB { return theme.RGB{R: r, G: g, B: b} }

// Aubergine is Slack's own sidebar colour, the Side ground when the
// workspace hasn't a theme of its own.
var Aubergine = rgb(0x4a, 0x15, 0x4b)

func inks(g theme.Ground) Inks {
	on := canvas.Style{}.Bg(g.BG)
	ink := func(c theme.RGB) canvas.Style { return on.Fg(g.Ink(c)) }
	text := g.Ink(rgb(226, 221, 211))
	lift := func(c theme.RGB) canvas.Style { return canvas.Style{}.Bg(g.Surface(c)).Fg(text) }
	return Inks{
		Ground: g.BG,
		Text:   ink(rgb(226, 221, 211)), Sub: ink(rgb(168, 162, 152)), Dim: ink(rgb(122, 117, 108)),
		Faint: ink(rgb(72, 68, 63)), Bright: ink(rgb(240, 236, 228)), Edge: ink(rgb(79, 73, 67)),
		Sel: lift(rgb(44, 40, 36)), Hover: lift(rgb(33, 31, 29)),
	}
}

// sideTint is how far the Side ground goes from the terminal's toward
// the workspace colour: a hint of aubergine, not Slack's full slab.
const sideTint = 0.3

// NewPalette makes the palette for the terminal's ground and the
// workspace's colour. The Side ground is the terminal's tinted toward
// the workspace colour (sideTint); rows on it lift by a step toward its
// text, as Slack's selected row does.
func NewPalette(g theme.Ground, workspace theme.RGB, colorBlind bool) Palette {
	side := theme.Ground{BG: theme.Mix(g.BG, workspace, sideTint), FG: g.FG}
	if !side.BG.Dark() {
		side.FG = theme.Light.FG
	}
	p := Palette{Main: inks(g), Side: inks(side)}
	p.Side.Sel = canvas.Style{}.Bg(theme.Mix(side.BG, side.FG, 0.14)).Fg(side.FG)
	p.Side.Hover = canvas.Style{}.Bg(theme.Mix(side.BG, side.FG, 0.07)).Fg(side.FG)

	accent := func(gr theme.Ground, c theme.RGB) canvas.Style { return canvas.Style{}.Bg(gr.BG).Fg(gr.Accent(c)) }
	green, red := rgb(127, 191, 138), rgb(224, 104, 92)
	if colorBlind {
		green, red = rgb(86, 180, 233), rgb(230, 159, 0)
	}
	p.Orange, p.Yellow, p.Blue = accent(g, rgb(217, 119, 87)), accent(g, rgb(229, 181, 103)), accent(g, rgb(143, 179, 217))
	p.Green, p.Red, p.Lavender = accent(g, green), accent(g, red), accent(g, rgb(178, 160, 214))
	p.NewRule = p.Orange.Fg(theme.Mix(p.Orange.FG, g.BG, 0.45))
	p.SideOrange, p.SideYellow, p.SideGreen = accent(side, rgb(217, 119, 87)), accent(side, rgb(229, 181, 103)), accent(side, green)

	text := g.Ink(rgb(226, 221, 211))
	surface := func(c theme.RGB) canvas.Style { return canvas.Style{}.Bg(g.Surface(c)).Fg(text) }
	p.Panel, p.Input, p.Chip = surface(rgb(30, 28, 26)), surface(rgb(40, 36, 32)), surface(rgb(56, 62, 72))
	p.Ask, p.Err = surface(rgb(44, 38, 24)), surface(rgb(46, 28, 26))

	// rush's code colours (convo/style.go), on the code block's panel.
	panel := theme.Ground{BG: p.Panel.BG, FG: g.FG}
	for c, rgbc := range [7]theme.RGB{rgb(168, 162, 152), rgb(204, 153, 205), rgb(163, 190, 140), rgb(222, 165, 132), rgb(137, 180, 222), rgb(120, 190, 175), rgb(122, 116, 108)} {
		p.Code[c] = canvas.Style{}.Bg(panel.BG).Fg(panel.Accent(rgbc))
	}
	return p
}
