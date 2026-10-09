package ui

import (
	"cmp"
	"encoding/json/jsontext"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/0xdeafcafe/loafer/internal/images"
	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/jsonx"
	"github.com/0xdeafcafe/photon/termimg"
	"github.com/0xdeafcafe/photon/theme"
)

// Pictures (internal/images): avatars and images as Kitty graphics where
// the terminal draws them (termimg.Drawn), else as before, initials and a
// "▣ alt" line. A picture is placeholders in a message's drawn rows, so it
// scrolls with them for nothing; when one lands, the drawn rows are let go
// and drawn again with it.

// pics makes the pictures; nil draws none.
var pics *images.Set

// picRows is the tallest an image is drawn.
const picRows = 8

type picsMsg struct{}

// startPics starts pictures where the terminal draws them, asking it its
// cell size so they keep their shape.
func (m *Model) startPics() tea.Cmd {
	// shortcut: every workspace's pictures are fetched with the first's
	// client; the d cookie is the browser's, shared by its workspaces, but
	// one signed in from another browser misses its files' pictures.
	if m.api == nil || !termimg.Drawn() || m.ws != nil && m.ws.i > 0 {
		return nil
	}
	pics = images.New(m.ctx, m.api.Fetch, images.Dir())
	return tea.Batch(tea.Raw(termimg.QueryCell), m.waitPics())
}

func (m *Model) waitPics() tea.Cmd {
	set := pics // read here, on the ui goroutine, not in the Cmd's
	return func() tea.Msg {
		select {
		case <-set.Landed():
			return picsMsg{}
		case <-m.ctx.Done():
			return nil
		}
	}
}

// onPics takes what pictures hear: one landing, or the cell's size in
// pixels. It says whether msg was one.
func (m *Model) onPics(msg tea.Msg) (tea.Cmd, bool) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case picsMsg:
		cmd = m.waitPics()
		if s := pics.Writes(); s != "" {
			cmd = tea.Batch(tea.Raw(s), cmd)
		}
	case uv.CellSizeEvent:
		termimg.SetCell(msg.Width, msg.Height)
	case uv.PixelSizeEvent:
		if termimg.Measured() || m.w == 0 || m.h == 0 {
			return nil, true
		}
		termimg.SetCell(msg.Width/m.w, msg.Height/m.h)
	default:
		return nil, false
	}
	m.drawn.Clear()
	m.claude.redraw()
	return cmd, true
}

// picture is url fitted to cols by rows cells, once it's landed.
func picture(url string, cols, rows int) (images.Pic, bool) {
	if pics == nil || url == "" || !termimg.Drawn() {
		return images.Pic{}, false
	}
	return pics.Get(url, cols, rows)
}

// picSeg is row r of p: its placeholders, coloured with its id.
func picSeg(p images.Pic, r int) canvas.Seg {
	id := theme.RGB{R: uint8(p.ID >> 16), G: uint8(p.ID >> 8), B: uint8(p.ID)}
	return canvas.Seg{Text: p.Cells[r], St: canvas.Style{}.Fg(id), W: p.Cols}
}

// face is the avatar on a message's first row, 2 cells by 1, once it's
// landed; else ok is false and the initials stay.
func face(v store.View, m *slack.Message) (canvas.Seg, bool) {
	url := ""
	if m.User != "" {
		url = v.Person(m.User).Avatar
	}
	if url == "" && m.BotProfile != nil {
		url = cmp.Or(m.BotProfile.Icons.Image72, m.BotProfile.Icons.Image48)
	}
	p, ok := picture(url, 2, 1)
	if !ok || p.Cols != 2 || p.Rows != 1 {
		return canvas.Seg{}, false
	}
	return picSeg(p, 0), true
}

// inlinePic is a context block's image as a picture a row high, once it's landed.
func inlinePic(url string) (canvas.Seg, bool) {
	p, ok := picture(url, 2, 1)
	if !ok || p.Rows != 1 {
		return canvas.Seg{}, false
	}
	return picSeg(p, 0), true
}

// repliers is a thread's repliers as 1-cell avatars, the first few that
// have landed, for the end of its "↩ N replies" line.
func repliers(p *Palette, v store.View, users []string) canvas.Row {
	r := canvas.Row{canvas.T("  ", p.Main.Text)}
	for _, u := range users[:min(len(users), 5)] {
		if pic, ok := picture(v.Person(u).Avatar, 1, 1); ok && pic.OK() {
			r = append(r, picSeg(pic, 0))
		}
	}
	if len(r) == 1 {
		return nil
	}
	return r
}

// pictureRows is an image at url as rows up to w wide: the picture once
// it's landed, else, while it's on its way and its size (iw by ih pixels)
// is known, as many blank rows as it'll take, so nothing moves when it
// lands. Nil when there's no picture to draw.
func pictureRows(url string, iw, ih, w int) []canvas.Row {
	p, ok := picture(url, w, picRows)
	switch {
	case ok && p.OK():
		out := make([]canvas.Row, p.Rows)
		for r := range out {
			out[r] = canvas.Row{picSeg(p, r)}
		}
		return out
	case ok || pics == nil || url == "" || !termimg.Drawn() || iw <= 0 || ih <= 0:
		return nil
	}
	cw, ch := termimg.Cell()
	_, n := termimg.Fit(iw, ih, w, picRows, cw, ch)
	return make([]canvas.Row, n)
}

// filePicture is an image file's picture, from its largest usual thumb.
func filePicture(raw jsontext.Value, w int) []canvas.Row {
	if pics == nil {
		return nil
	}
	var f struct {
		URL      string `json:"url_private"`
		Thumb360 string `json:"thumb_360"`
		Thumb480 string `json:"thumb_480"`
		Thumb720 string `json:"thumb_720"`
		W        int    `json:"original_w"`
		H        int    `json:"original_h"`
	}
	if jsonx.Unmarshal(raw, &f) != nil {
		return nil
	}
	return pictureRows(cmp.Or(f.Thumb720, f.Thumb480, f.Thumb360, f.URL), f.W, f.H, w)
}
