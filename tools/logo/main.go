// Command logo previews loafer's logo options: a loafer in Slack's colours,
// drawn several ways, and the event animations it can play.
//
//	go run .                  every variant, on the dark, light and aubergine grounds
//	go run . -v N             variant N in a mock loafer header
//	go run . -v N -a NAME     that header playing NAME three times
//	go run . -v N -a all      every animation in turn
//	go run . -v N -a NAME -frames   every frame of NAME, laid out still
//	go run . -list            the variants and animations
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// ---- colour ----

type rgb struct {
	R, G, B uint8
	ok      bool // false: nothing here, the ground shows
}

func hex(v uint32) rgb     { return rgb{uint8(v >> 16), uint8(v >> 8), uint8(v), true} }
func c3(r, g, b uint8) rgb { return rgb{r, g, b, true} }

var (
	red, yellow, green, blue = hex(0xE01E5A), hex(0xECB22E), hex(0x2EB67D), hex(0x36C5F0)
	aubergine                = hex(0x4A154B)
	white, black             = hex(0xFFFFFF), hex(0x000000)
	brands                   = []rgb{red, yellow, green, blue}
)

func mix(a, b rgb, t float64) rgb {
	t = min(1, max(0, t))
	l := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return rgb{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B), true}
}

func lum(c rgb) float64 {
	ch := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
}

func contrast(a, b rgb) float64 {
	la, lb := lum(a), lum(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

func (c rgb) dark() bool { return contrast(c, white) > contrast(c, black) }
func (c rgb) fg() string { return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.R, c.G, c.B) }
func (c rgb) bg() string { return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", c.R, c.G, c.B) }

func dist(a, b rgb) int {
	dr, dg, db := int(a.R)-int(b.R), int(a.G)-int(b.G), int(a.B)-int(b.B)
	return dr*dr + dg*dg + db*db
}

// ground is what the logo is drawn on: a background and its text.
type ground struct {
	name   string
	bg, fg rgb
}

var (
	dark      = ground{"dark 17,16,14", c3(17, 16, 14), c3(226, 221, 211)}
	light     = ground{"light 250,249,245", c3(250, 249, 245), c3(40, 37, 32)}
	workspace = ground{"aubergine 4A154B (header)", aubergine, c3(226, 221, 211)}
	grounds   = []ground{dark, light, workspace}
)

// pal is the colours for one frame on one ground.
type pal struct {
	g   ground
	rot float64 // the colour cycle: brand colours moved this far along red, yellow, green, blue
}

// fix pushes c away from the ground until it stands out by want, as
// rush's theme.Accent does: lighter on a dark ground, darker on a light one.
func (p *pal) fix(c rgb, want float64) rgb {
	end := white
	if !p.g.bg.dark() {
		end = black
	}
	out := c
	for t := .05; contrast(out, p.g.bg) < want && t <= 1; t += .05 {
		out = mix(c, end, t)
	}
	return out
}

// brand is Slack colour i (0 red, 1 yellow, 2 green, 3 blue), moved along
// by the cycle and blended between neighbours; 3:1 on the ground.
func (p *pal) brand(i float64) rgb {
	i = math.Mod(math.Mod(i-p.rot, 4)+4, 4)
	a := int(i)
	return p.fix(mix(brands[a], brands[(a+1)%4], i-float64(a)), 3)
}

// grad is red to blue across t, 0 to 1.
func (p *pal) grad(t float64) rgb { return p.brand(t * 3) }

// sole is aubergine, lifted on a dark ground so it still shows.
func (p *pal) sole() rgb { return p.fix(aubergine, 2) }

// shade is c taken toward black: a strap's shadow, a seam.
func shade(c rgb, k float64) rgb { return mix(c, black, k) }

// ---- variants ----

// kind is how a variant's pixels map to cells: sx by sy pixels a cell.
type kind struct{ sx, sy int }

var (
	kHalf = kind{1, 2} // ▀ ▄ █
	kQuad = kind{2, 2} // ▘ ▝ ▖ ▗ ▚ ▞ …
	kSext = kind{2, 3} // U+1FB00 sextants
	kBrl  = kind{2, 4} // ⣿
	kCell = kind{1, 1} // a glyph a cell: box drawing
)

// A variant is pixel art, a letter a pixel ('.' none), and how each
// letter is coloured. The letters animations know: S strap, P penny (and
// 1-4, a four-colour penny), O sole, H heel; anything else is leather.
type variant struct {
	name, desc string
	k          kind
	art        []string
	glyph      []string                               // kCell: the glyph each cell shows
	ink        func(p *pal, b byte, x, y float64) rgb // x, y: 0 to 1 across the art
}

func isPenny(b byte) bool { return b == 'P' || b >= '1' && b <= '4' }
func isSole(b byte) bool  { return b == 'O' || b == 'H' }

var variants = []variant{
	{
		name: "bands", k: kHalf,
		desc: "half blocks; the upper in four bands heel to toe, a dark strap, a bright penny",
		art: []string{
			"..................",
			"......UUU.........",
			".UU..UUUUUU.......",
			"UUUUSSSPPSSUUU....",
			"UUUUUUUUUUUUUUUU..",
			"UUUUUUUUUUUUUUUUUU",
			"OOOOOOOOOOOOOOOOOO",
			"HHHH..............",
		},
		ink: func(p *pal, b byte, x, y float64) rgb {
			band := p.brand(math.Floor(x * 4))
			switch b {
			case 'S':
				return shade(band, .45)
			case 'P':
				return c3(250, 244, 228)
			case 'O', 'H':
				return p.sole()
			}
			return band
		},
	},
	{
		name: "slack penny", k: kHalf,
		desc: "half blocks; aubergine leather with Slack's four-colour mark as the penny",
		art: []string{
			"..................",
			"......AAA.........",
			".AA..SS12SSA......",
			"AAAASSS34SSAAA....",
			"AAAAAAAAAAAAAAAA..",
			"AAAAAAAAAAAAAAAAAA",
			"OOOOOOOOOOOOOOOOOO",
			"HHHH..............",
		},
		ink: func(p *pal, b byte, x, y float64) rgb {
			leather := p.fix(aubergine, 2.2)
			switch b {
			case 'S':
				return p.fix(mix(aubergine, white, .12), 1.6)
			case '1':
				return p.brand(3)
			case '2':
				return p.brand(2)
			case '3':
				return p.brand(0)
			case '4':
				return p.brand(1)
			case 'O', 'H':
				return mix(p.g.fg, p.g.bg, .25)
			}
			return leather
		},
	},
	{
		name: "gradient", k: kQuad,
		desc: "quadrants; one smooth red-to-blue upper, an aubergine strap and sole, a gold penny",
		art: []string{
			"....................................",
			"...........UUUUUUUU.................",
			".UUUUU....UUUUPPUUUUUUU.............",
			"UUUUUUUUUSSSSSSSSSSSUUUUUUUU........",
			"UUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUU....",
			"UUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUU",
			"OOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOO",
			"OOOOOOOO............................",
		},
		ink: func(p *pal, b byte, x, y float64) rgb {
			switch b {
			case 'S':
				return p.sole()
			case 'P':
				return p.fix(c3(250, 214, 120), 3)
			case 'O', 'H':
				return p.sole()
			}
			return p.grad(x)
		},
	},
	{
		name: "smooth", k: kSext,
		desc: "sextants; a finer profile in parts: green quarter, blue vamp, red strap, gold penny",
		art: []string{
			"....................................",
			"....................................",
			"...............VVVVV................",
			".QQQQQ........SSPPSSSV..............",
			"QQQQQQQQ.....QSSPPSSSVVV............",
			"QQQQQQQQQQQQQQVVVVVVVVVVVVV.........",
			"QQQQQQQQQQQQQQVVVVVVVVVVVVVVVV......",
			"QQQQQQQQQQQQQQVVVVVVVVVVVVVVVVVVV...",
			"QQQQQQQQQQQQQQVVVVVVVVVVVVVVVVVVVVVV",
			"OOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOO",
			"HHHHHHHH............................",
			"HHHHHHHH............................",
		},
		ink: func(p *pal, b byte, x, y float64) rgb {
			switch b {
			case 'Q':
				return p.brand(2)
			case 'V':
				return p.brand(3)
			case 'S':
				return p.brand(0)
			case 'P':
				return p.brand(1)
			}
			return p.sole()
		},
	},
	{
		name: "line", k: kCell,
		desc: "box drawing, three rows; a low rounded outline, strap and penny on the vamp, a block heel",
		glyph: []string{
			"                   ",
			"╭──╮    ╭──────╮   ",
			"│  ╰────╯ ═●═  ╰──╮",
			"╰█▄▄━━━━━━━━━━━━━━╯",
		},
		art: []string{
			"...................",
			"QQQQ....VVVVVVVV...",
			"Q..QQQQQV.SPS..VVVV",
			"QHHHOOOOOOOOOOOOOOO",
		},
		ink: func(p *pal, b byte, x, y float64) rgb {
			switch b {
			case 'Q':
				return p.brand(0)
			case 'V':
				return p.brand(3)
			case 'S', 'P':
				return p.brand(1)
			}
			return p.brand(2)
		},
	},
	{
		name: "braille", k: kBrl,
		desc: "braille; a fine dotted outline shading red to blue, the strap and sole filled",
		art: []string{
			"......................................",
			"......................................",
			"...............######.................",
			"..###.........#......##...............",
			".#...##......#.SSPPSS..###............",
			".#.....##...#..SSPPSS.....###.........",
			".#.......###................##........",
			".#............................##......",
			".#..............................##....",
			".#................................#...",
			".#.................................#..",
			".#..................................#.",
			".OOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOO.",
			".OOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOO",
			".HHHHHH...............................",
			".HHHHHH...............................",
		},
		ink: func(p *pal, b byte, x, y float64) rgb {
			switch b {
			case 'P':
				return p.fix(c3(250, 214, 120), 3)
			case 'O', 'H':
				return p.sole()
			}
			return p.grad(x)
		},
	},
	{
		name: "mini", k: kSext,
		desc: "sextants, two rows; one red silhouette, heel and arch cut from it, a gold penny",
		art: []string{
			"......................",
			"......................",
			"......................",
			".......UUU............",
			".UU...UUUUUU..........",
			"UUUUUUUUPPUUUUU.......",
			"UUUUUUUUUUUUUUUUUUU...",
			"UUUUUUUUUUUUUUUUUUUUU.",
			"UUUU.......UUUUUUUUU..",
			"......................",
			"......................",
			"......................",
		},
		ink: func(p *pal, b byte, x, y float64) rgb {
			switch b {
			case 'P':
				return p.brand(1)
			}
			return p.brand(0)
		},
	},
}

// check is the variants' self-check: every art is a full canvas, and a
// glyph layer matches its art.
func check() {
	for i, v := range variants {
		if len(v.art) != 4*v.k.sy {
			panic(fmt.Sprintf("variant %d: %d pixel rows, want %d", i+1, len(v.art), 4*v.k.sy))
		}
		for y, row := range v.art {
			if len(row) != len(v.art[0]) || len(row)%v.k.sx != 0 {
				panic(fmt.Sprintf("variant %d row %d: width %d", i+1, y, len(row)))
			}
			if v.glyph != nil && utf8.RuneCountInString(v.glyph[y]) != len(row) {
				panic(fmt.Sprintf("variant %d row %d: glyphs %d, art %d", i+1, y, utf8.RuneCountInString(v.glyph[y]), len(row)))
			}
		}
	}
}

// ---- pixels ----

// img is a frame's pixels, with a cell of room either side for what
// moves or flies off.
type img struct {
	w, h int
	c    []rgb
	b    []byte // the art's letter, 0 for none
	r    []rune // kCell: the glyph
}

func newImg(w, h int) *img {
	return &img{w: w, h: h, c: make([]rgb, w*h), b: make([]byte, w*h), r: make([]rune, w*h)}
}

func (m *img) at(x, y int) int {
	if x < 0 || y < 0 || x >= m.w || y >= m.h {
		return -1
	}
	return y*m.w + x
}

func (v *variant) build(p *pal) *img {
	aw, ah := len(v.art[0]), len(v.art)
	m := newImg(aw+2*v.k.sx, ah)
	for y, row := range v.art {
		var gl []rune
		if v.glyph != nil {
			gl = []rune(v.glyph[y])
		}
		for x := range len(row) {
			if row[x] == '.' {
				continue
			}
			i := m.at(x+v.k.sx, y)
			m.b[i] = row[x]
			m.c[i] = v.ink(p, row[x], (float64(x)+.5)/float64(aw), (float64(y)+.5)/float64(ah))
			if gl != nil {
				m.r[i] = gl[x]
			}
		}
	}
	return m
}

// moved is m slid dx across, each column lifted up(x): columns move
// whole, so the shoe bends without tearing.
func (m *img) moved(dx int, up func(x int) int) *img {
	n := newImg(m.w, m.h)
	for y := range m.h {
		for x := range m.w {
			s := m.at(x, y)
			if m.b[s] == 0 {
				continue
			}
			if d := n.at(x+dx, y-up(x)); d >= 0 {
				n.c[d], n.b[d], n.r[d] = m.c[s], m.b[s], m.r[s]
			}
		}
	}
	return n
}

// span is the first and last columns with anything in them.
func (m *img) span() (x0, x1 int) {
	x0, x1 = m.w, -1
	for i, b := range m.b {
		if b != 0 {
			x0, x1 = min(x0, i%m.w), max(x1, i%m.w)
		}
	}
	return
}

// ---- cells ----

type tcell struct {
	r      rune
	fg, bg rgb // bg not ok: the ground
}

var (
	halfGlyphs = []rune(" ▀▄█")
	quadGlyphs = []rune(" ▘▝▀▖▌▞▛▗▚▐▜▄▙▟█")
)

func sextGlyph(n int) rune {
	switch n {
	case 0:
		return ' '
	case 63:
		return '█'
	case 21:
		return '▌'
	case 42:
		return '▐'
	}
	d := 1
	if n > 21 {
		d++
	}
	if n > 42 {
		d++
	}
	return rune(0x1FB00 + n - d)
}

// brlBit is the braille dot for sub-pixel i, row-major two wide.
var brlBit = [8]int{0, 3, 1, 4, 2, 5, 6, 7}

// pack turns pixels into cells.
func pack(m *img, k kind, g ground) [][]tcell {
	cols, rows := m.w/k.sx, m.h/k.sy
	out := make([][]tcell, rows)
	for cy := range rows {
		out[cy] = make([]tcell, cols)
		for cx := range cols {
			var px []rgb
			var gl rune
			for y := range k.sy {
				for x := range k.sx {
					i := m.at(cx*k.sx+x, cy*k.sy+y)
					px = append(px, m.c[i])
					if m.r[i] != 0 {
						gl = m.r[i]
					}
				}
			}
			out[cy][cx] = cellOf(px, k, gl, g)
		}
	}
	return out
}

// cellOf is one cell from its pixels. A cell has two colours at most, so
// it keeps the pair that loses the least.
func cellOf(px []rgb, k kind, gl rune, g ground) tcell {
	switch k {
	case kCell:
		if !px[0].ok {
			return tcell{r: ' '}
		}
		return tcell{r: gl, fg: px[0]}
	case kBrl:
		bits, count := 0, map[rgb]int{}
		var best rgb
		for i, c := range px {
			if c.ok {
				bits |= 1 << brlBit[i]
				if count[c]++; count[c] > count[best] {
					best = c
				}
			}
		}
		if bits == 0 {
			return tcell{r: ' '}
		}
		return tcell{r: rune(0x2800 + bits), fg: best}
	}
	solid := func(c rgb) rgb { // what a pixel looks like, the ground where it's empty
		if !c.ok {
			return g.bg
		}
		return c
	}
	var cols []rgb
	for _, c := range px {
		if !slices.Contains(cols, c) {
			cols = append(cols, c)
		}
	}
	if len(cols) == 1 {
		if !cols[0].ok {
			return tcell{r: ' '}
		}
		return tcell{r: '█', fg: cols[0]}
	}
	a, b, least := cols[0], cols[1], math.MaxInt
	for i := range cols {
		for j := i + 1; j < len(cols); j++ {
			loss := 0
			for _, c := range px {
				loss += min(dist(solid(c), solid(cols[i])), dist(solid(c), solid(cols[j])))
			}
			if loss < least {
				a, b, least = cols[i], cols[j], loss
			}
		}
	}
	if !a.ok { // the ground is always the background
		a, b = b, a
	}
	mask := 0
	for i, c := range px {
		if dist(solid(c), solid(a)) <= dist(solid(c), solid(b)) {
			mask |= 1 << i
		}
	}
	var r rune
	switch k {
	case kHalf:
		r = halfGlyphs[mask]
	case kQuad:
		r = quadGlyphs[mask]
	default:
		r = sextGlyph(mask)
	}
	return tcell{r: r, fg: a, bg: b}
}

// ---- frames ----

// spark is something drawn off the shoe, in cells: dust, a glint, a tick.
type spark struct {
	x, y int
	r    rune
	c    rgb
}

type frame struct {
	v  *variant
	p  *pal
	m  *img
	fx []spark
}

// toCell is the cell a pixel column falls in.
func (f *frame) toCell(x int) int { return x / f.v.k.sx }

// draw is variant v on ground g, f frames into a (nil: still).
func draw(v *variant, g ground, a *anim, f int) []string {
	p := &pal{g: g}
	if a != nil && a.pre != nil {
		a.pre(p, f, a.n)
	}
	fr := &frame{v: v, p: p, m: v.build(p)}
	if a != nil && a.post != nil {
		a.post(fr, f, a.n)
	}
	cells := pack(fr.m, v.k, g)
	for _, s := range fr.fx {
		if s.y >= 0 && s.y < len(cells) && s.x >= 0 && s.x < len(cells[s.y]) && cells[s.y][s.x].r == ' ' && !cells[s.y][s.x].bg.ok {
			cells[s.y][s.x] = tcell{r: s.r, fg: s.c}
		}
	}
	return emit(cells, g)
}

// emit writes cells as truecolour ANSI, the ground behind every one.
func emit(cells [][]tcell, g ground) []string {
	out := make([]string, len(cells))
	for y, row := range cells {
		var sb strings.Builder
		var curF, curB rgb
		for _, c := range row {
			bg := g.bg
			if c.bg.ok {
				bg = c.bg
			}
			if bg != curB {
				sb.WriteString(bg.bg())
				curB = bg
			}
			if c.r != ' ' && c.fg != curF {
				sb.WriteString(c.fg.fg())
				curF = c.fg
			}
			sb.WriteRune(c.r)
		}
		sb.WriteString("\x1b[0m")
		out[y] = sb.String()
	}
	return out
}

// ---- animations ----

// anim is a reaction to one event. pre sets the frame's colours before
// the shoe is drawn; post moves it, lights it and adds what flies off.
type anim struct {
	name, desc, event string
	n                 int
	every             time.Duration
	pre               func(p *pal, f, n int)
	post              func(fr *frame, f, n int)
}

// ease is a smooth 0 to 1.
func ease(t float64) float64 {
	t = min(1, max(0, t))
	return t * t * (3 - 2*t)
}

func px(cells float64, per int) int { return int(math.Round(cells * float64(per))) }

// dust is a fading colour for what comes off the shoe.
func dust(p *pal, k float64) rgb { return mix(p.g.fg, p.g.bg, .35+.6*k) }

var anims = []anim{
	{
		name: "tap", event: "a mention arrives",
		desc: "the toe lifts and taps down twice, a tick off the toe each time",
		n:    10, every: 60 * time.Millisecond,
		post: func(fr *frame, f, n int) {
			lift := []float64{0, .35, .6, .6, 0, .35, .6, .6, 0, 0}[f]
			x0, x1 := fr.m.span()
			pivot := x0 + (x1-x0)/2 // the ball of the foot: the throat stays put
			up := px(lift, fr.v.k.sy)
			if fr.v.k == kCell { // a line can't bend by a whole row: the toe lights instead
				for i, b := range fr.m.b {
					if b != 0 && i%fr.m.w > pivot+(x1-pivot)/2 {
						fr.m.c[i] = mix(fr.m.c[i], fr.p.brand(1), lift)
					}
				}
				up = 0
			}
			fr.m = fr.m.moved(0, func(x int) int {
				if x <= pivot {
					return 0
				}
				return int(math.Round(float64(up) * float64(x-pivot) / float64(x1-pivot)))
			})
			if f == 4 || f == 8 || f == 5 || f == 9 {
				k := 0.0
				if f == 5 || f == 9 {
					k = .6
				}
				tx := fr.toCell(x1) + 1
				fr.fx = append(fr.fx, spark{tx, 3, '·', mix(fr.p.brand(1), fr.p.g.bg, k)}, spark{tx, 2, '˙', mix(fr.p.brand(1), fr.p.g.bg, k+.2)})
			}
		},
	},
	{
		name: "glint", event: "your message is sent",
		desc: "a band of light sweeps across the leather, heel to toe",
		n:    14, every: 60 * time.Millisecond,
		post: func(fr *frame, f, n int) {
			m, k := fr.m, fr.v.k
			pos := -2 + float64(m.w/k.sx+4)*float64(f)/float64(n-1)
			for i, b := range m.b {
				if b == 0 || isSole(b) {
					continue
				}
				x, y := float64(i%m.w)/float64(k.sx), float64(i/m.w)/float64(k.sy)
				if d := math.Abs(x + y*.8 - pos); d < 1.6 {
					m.c[i] = mix(m.c[i], white, .8*(1-d/1.6))
				}
			}
		},
	},
	{
		name: "cycle", event: "reconnected",
		desc: "the four colours roll once along the shoe, heel to toe, and settle",
		n:    16, every: 60 * time.Millisecond,
		pre: func(p *pal, f, n int) { p.rot = 4 * ease(float64(f)/float64(n-1)) },
	},
	{
		name: "step", event: "sending a message",
		desc: "the heel lifts, the shoe steps forward, plants with a puff, slides home",
		n:    14, every: 60 * time.Millisecond,
		post: func(fr *frame, f, n int) {
			heel := []float64{0, .3, .5, .5, .5, .3, 0, 0, 0, 0, 0, 0, 0, 0}[f]
			fwd := []float64{0, 0, 0, .5, 1, 1, 1, 1, 1, 1, .75, .5, .25, 0}[f]
			x0, x1 := fr.m.span()
			pivot := x0 + (x1-x0)*65/100
			up := px(heel, fr.v.k.sy)
			if fr.v.k == kCell { // a line can't bend by a whole row: it only slides
				up = 0
			}
			fr.m = fr.m.moved(px(fwd, fr.v.k.sx), func(x int) int {
				if x >= pivot {
					return 0
				}
				return int(math.Round(float64(up) * float64(pivot-x) / float64(pivot-x0)))
			})
			if f >= 6 && f <= 8 {
				k := float64(f-6) / 3
				fr.fx = append(fr.fx, spark{0, 3, '·', dust(fr.p, k)})
				if f >= 7 {
					fr.fx = append(fr.fx, spark{0, 2, '˙', dust(fr.p, k+.2)})
				}
			}
		},
	},
	{
		name: "hop", event: "startup, once loaded",
		desc: "a small hop, toe up in the air, landing with a puff of dust each side",
		n:    13, every: 60 * time.Millisecond,
		post: func(fr *frame, f, n int) {
			lift := []float64{0, .25, .5, .5, .5, .25, 0, 0, 0, 0, 0, 0, 0}[f]
			tilt := []float64{0, .2, .3, .3, .2, 0, 0, 0, 0, 0, 0, 0, 0}[f]
			x0, x1 := fr.m.span()
			up, tl := px(lift, fr.v.k.sy), float64(px(tilt, fr.v.k.sy))
			fr.m = fr.m.moved(0, func(x int) int {
				return up + int(math.Round(tl*float64(x-x0)/float64(x1-x0)))
			})
			if f >= 6 && f <= 11 {
				k := float64(f-6) / 6
				w := fr.m.w / fr.v.k.sx
				r := []rune("·∙·˙˙ ")[f-6]
				fr.fx = append(fr.fx, spark{0, 3, r, dust(fr.p, k)}, spark{w - 1, 3, r, dust(fr.p, k)})
			}
		},
	},
	{
		name: "flip", event: "a mention arrives (quieter)",
		desc: "the penny flips in its slot, edge on, copper back, and face up again; a four-colour penny spins",
		n:    12, every: 70 * time.Millisecond,
		post: func(fr *frame, f, n int) {
			m := fr.m
			var idx []int
			x0, x1 := m.w, -1
			for i, b := range m.b {
				if isPenny(b) {
					idx = append(idx, i)
					x0, x1 = min(x0, i%m.w), max(x1, i%m.w)
				}
			}
			if len(idx) == 0 {
				return
			}
			if m.b[idx[0]] != 'P' { // the four-colour penny turns a quarter a frame
				var cs []rgb
				for _, i := range idx { // reading order: 1 2 / 3 4, round as 1 2 4 3
					cs = append(cs, m.c[i])
				}
				ring := []int{0, 1, 3, 2}
				for j := range ring {
					m.c[idx[ring[j]]] = cs[ring[(j+f%4)%4]]
				}
				return
			}
			// Every penny is two pixels wide: face up, then edge on, then
			// the copper back, then edge on, then face up again.
			c := math.Cos(math.Pi * float64(f) / 6)
			face := m.c[idx[0]]
			if c < 0 {
				face = fr.p.fix(c3(200, 110, 60), 3)
			}
			if math.Abs(c) < .6 {
				face = mix(face, fr.p.g.fg, .5)
			}
			for _, i := range idx {
				if m.r[i] != 0 { // a glyph penny turns by glyph
					m.r[i] = []rune("●◖▮◗●◖▮◗●◖▮◗")[f]
				}
				m.c[i] = face
				if math.Abs(c) < .6 && m.r[i] == 0 && i%m.w == x1 { // edge on, it's one pixel thin
					m.c[i] = m.c[i+1]
				}
			}
		},
	},
	{
		name: "lace", event: "startup",
		desc: "drawn in: the sole runs heel to toe, the upper sweeps after it, strap, then the penny glints",
		n:    18, every: 60 * time.Millisecond,
		post: func(fr *frame, f, n int) {
			m := fr.m
			x0, x1 := m.span()
			t := float64(f) / float64(n-5)
			for i, b := range m.b {
				if b == 0 {
					continue
				}
				x := float64(i%m.w-x0) / float64(x1-x0)
				y := float64(i/m.w) / float64(m.h)
				show := false
				switch {
				case isSole(b):
					show = x <= t*2.2
				case b == 'S':
					show = f >= n-6
				case isPenny(b):
					show = f >= n-4
				default:
					e := t*1.5 - .3 - x - (1-y)*.25
					show = e >= 0
					if show && e < .12 {
						m.c[i] = mix(m.c[i], white, .55)
					}
				}
				if !show {
					m.c[i], m.b[i], m.r[i] = rgb{}, 0, 0
				}
			}
			if f >= n-4 && f <= n-2 {
				for i, b := range fr.v.build(fr.p).b {
					if isPenny(b) {
						fr.fx = append(fr.fx, spark{fr.toCell(i % m.w), i/m.w/fr.v.k.sy - 1, []rune("✦·˙")[f-(n-4)], fr.p.brand(1)})
						break
					}
				}
			}
		},
	},
	{
		name: "shake", event: "a send failed, or signed out",
		desc: "a quick shake side to side, washing red and easing back",
		n:    10, every: 50 * time.Millisecond,
		post: func(fr *frame, f, n int) {
			dx := []float64{.5, -.5, .5, -.5, .5, -.5, .25, 0, 0, 0}[f]
			k := .6 * max(0, 1-float64(f)/8)
			for i := range fr.m.c {
				if fr.m.b[i] != 0 {
					fr.m.c[i] = mix(fr.m.c[i], fr.p.brand(0), k)
				}
			}
			fr.m = fr.m.moved(px(dx, fr.v.k.sx), func(int) int { return 0 })
		},
	},
}

// ---- screens ----

const headW = 100

var (
	cText, cSub, cDim, cFaint = c3(226, 221, 211), c3(168, 162, 152), c3(122, 117, 108), c3(72, 68, 63)
	cOrange, cYellow, cGreen  = c3(217, 119, 87), c3(229, 181, 103), c3(127, 191, 138)
)

// seg is s in fg on the dark ground, with SGR attributes extra.
func seg(fg rgb, extra, s string) string {
	return "\x1b[0m" + dark.bg.bg() + fg.fg() + extra + s
}

// width is s's width in cells, its escapes skipped.
func width(s string) int {
	n, esc := 0, false
	for _, r := range s {
		switch {
		case r == 0x1b:
			esc = true
		case esc:
			esc = !(r >= 0x40 && r <= 0x7e && r != '[')
		default:
			n++
		}
	}
	return n
}

// row is left and right on one full-width row of the dark ground.
func row(left, right string) string {
	gap := max(1, headW-width(left)-width(right))
	return left + seg(cText, "", strings.Repeat(" ", gap)) + right + "\x1b[0m"
}

// header is loafer's header with logo lines beside the wordmark and tabs.
func header(logo []string) []string {
	lead := seg(cText, "", "  ")
	gap := seg(cText, "", "  ")
	word := seg(cText, "\x1b[1;3m", "loafer") + seg(cSub, "", "  LangWatch ▾") +
		seg(cYellow, "\x1b[1m", "    @ 3 mentions") + seg(cDim, "", "   12 unread")
	status := seg(cGreen, "", "● live") + seg(cSub, "", "   alex  ")
	var tabs string
	for i, t := range []string{"Home", "DMs ·2", "Activity ·3", "Later ·5", "Claude"} {
		if i == 0 {
			tabs += "\x1b[0m\x1b[1;38;2;24;22;20;48;2;217;119;87m " + t + " "
		} else {
			tabs += seg(cSub, "", " ") + "\x1b[0m" + c3(40, 37, 34).bg() + cSub.fg() + " " + t + " "
		}
	}
	hint := seg(cText, "\x1b[1m", "ctrl+k") + seg(cDim, "", " jump  ")
	return []string{
		row(lead+logo[0], ""),
		row(lead+logo[1]+gap+word, status),
		row(lead+logo[2]+gap+tabs, hint),
		row(lead+logo[3], ""),
		seg(cFaint, "", strings.Repeat("─", headW)) + "\x1b[0m",
	}
}

func gallery() {
	fmt.Println("\x1b[1mloafer logo options\x1b[0m  each variant on the dark and light grounds, and on the aubergine header ground (ui.md rule 2a)")
	fmt.Println()
	wide := 0
	for _, v := range variants {
		wide = max(wide, len(v.art[0])/v.k.sx+2)
	}
	block := func(g ground, s string) string {
		return g.bg.bg() + "  " + s + g.bg.bg() + strings.Repeat(" ", wide-width(s)) + "  \x1b[0m"
	}
	var heads []string
	for _, g := range grounds {
		heads = append(heads, fmt.Sprintf("%-*s", wide+4, g.name))
	}
	fmt.Println("     \x1b[2m" + strings.Join(heads, "  ") + "\x1b[0m")
	for i, v := range variants {
		fmt.Printf("\x1b[1m%2d  %s\x1b[0m  \x1b[2m%s\x1b[0m\n", i+1, v.name, v.desc)
		logos := make([][]string, len(grounds))
		for j, g := range grounds {
			logos[j] = draw(&variants[i], g, nil, 0)
		}
		for y := -1; y <= 4; y++ {
			var parts []string
			for j, g := range grounds {
				s := ""
				if y >= 0 && y < 4 {
					s = logos[j][y]
				}
				parts = append(parts, block(g, s))
			}
			fmt.Println("     " + strings.Join(parts, "  "))
		}
		fmt.Println()
	}
}

func list() {
	fmt.Println("variants (-v N)")
	for i, v := range variants {
		fmt.Printf("  %d  %-12s %s\n", i+1, v.name, v.desc)
	}
	fmt.Println("\nanimations (-a NAME, or -a all)")
	for _, a := range anims {
		fmt.Printf("  %-6s %4dms  %-30s %s\n", a.name, a.n*int(a.every/time.Millisecond), a.event, a.desc)
	}
}

func findAnim(name string) *anim {
	for i := range anims {
		if anims[i].name == name {
			return &anims[i]
		}
	}
	return nil
}

// frames lays every frame of each animation out still, in rows.
func frames(v *variant, as []*anim) {
	w := len(v.art[0])/v.k.sx + 2
	per := max(1, headW/(w+3))
	for _, a := range as {
		fmt.Printf("\x1b[1m%s\x1b[0m  \x1b[2m%d frames × %dms · %s · %s\x1b[0m\n", a.name, a.n, a.every/time.Millisecond, a.event, a.desc)
		for s := 0; s < a.n; s += per {
			e := min(a.n, s+per)
			var lines [5]string
			for f := s; f < e; f++ {
				lines[0] += fmt.Sprintf("%-*s", w+3, fmt.Sprintf("%d · %dms", f, f*int(a.every/time.Millisecond)))
				for y, l := range draw(v, dark, a, f) {
					lines[y+1] += l + "   "
				}
			}
			for _, l := range lines {
				fmt.Println(l)
			}
		}
		fmt.Println()
	}
}

// play runs animations in the mock header on the alternate screen.
func play(v *variant, as []*anim, times int) {
	restore := func() { fmt.Print("\x1b[0m\x1b[?25h\x1b[?1049l") }
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sig
		restore()
		os.Exit(130)
	}()
	defer restore()
	fmt.Print("\x1b[?1049h\x1b[?25l" + dark.bg.bg() + "\x1b[2J")
	show := func(logo []string, label string) {
		var sb strings.Builder
		sb.WriteString("\x1b[H")
		for y, l := range append([]string{row("", "")}, header(logo)...) {
			fmt.Fprintf(&sb, "\x1b[%d;1H%s", y+1, l)
		}
		fmt.Fprintf(&sb, "\x1b[8;1H%s", row(label, ""))
		fmt.Print(sb.String())
	}
	still := draw(v, dark, nil, 0)
	for _, a := range as {
		for k := range times {
			label := seg(cText, "\x1b[1m", "  "+a.name) + seg(cDim, "", fmt.Sprintf("  %s · %s · %d of %d", a.event, a.desc, k+1, times))
			show(still, label)
			time.Sleep(time.Second)
			for f := range a.n {
				show(draw(v, dark, a, f), label)
				time.Sleep(a.every)
			}
		}
	}
	show(still, "")
	time.Sleep(time.Second)
}

func main() {
	check()
	vn := flag.Int("v", 0, "variant `N` in the mock header")
	an := flag.String("a", "", "animation `NAME` to play, or all")
	ls := flag.Bool("list", false, "list the variants and animations")
	fr := flag.Bool("frames", false, "with -a, lay every frame out still instead of playing")
	flag.Parse()
	if *ls {
		list()
		return
	}
	if *vn == 0 && *an == "" {
		gallery()
		return
	}
	if *vn == 0 {
		*vn = 1
	}
	if *vn < 1 || *vn > len(variants) {
		fmt.Fprintf(os.Stderr, "no variant %d: there are %d (go run . -list)\n", *vn, len(variants))
		os.Exit(2)
	}
	v := &variants[*vn-1]
	if *an == "" {
		for _, l := range header(draw(v, dark, nil, 0)) {
			fmt.Println(l)
		}
		return
	}
	var as []*anim
	if *an == "all" {
		for i := range anims {
			as = append(as, &anims[i])
		}
	} else if a := findAnim(*an); a != nil {
		as = []*anim{a}
	} else {
		fmt.Fprintf(os.Stderr, "no animation %q (go run . -list)\n", *an)
		os.Exit(2)
	}
	if *fr {
		frames(v, as)
		return
	}
	times := 3
	if *an == "all" {
		times = 2
	}
	play(v, as, times)
}
