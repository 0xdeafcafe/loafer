// Package images is avatars and pictures as Kitty graphics (photon/termimg),
// made off the UI goroutine. Each is fetched once, fitted to the cells it
// takes, and kept on disk as a small PNG by its URL's hash; the last few
// hundred are kept in memory, and one asked for again while it's on its
// way is fetched only the once. Landed says when one's ready, and Writes
// is what to send the terminal, raw, so it can draw them.
package images

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/0xdeafcafe/photon/rows"
	"github.com/0xdeafcafe/photon/termimg"
)

// keep is how many pictures are kept in memory.
const keep = 300

// Pic is a picture made: its Kitty image id and the cells it takes. Each
// of Cells is a row of placeholders without colour; the id goes as that
// row's foreground. The zero Pic is one that couldn't be made.
type Pic struct {
	ID         uint32
	Cols, Rows int
	Cells      []string
}

// OK is whether the picture could be made.
func (p Pic) OK() bool { return p.Cols > 0 }

// Fetch GETs a URL, with whatever credentials its host needs.
type Fetch func(ctx context.Context, url string) ([]byte, error)

// key is a picture asked for: where from, the most cells it may take, and
// a cell's size in pixels.
type key struct {
	url        string
	cols, rows int
	cw, ch     int
}

type Set struct {
	ctx    context.Context
	fetch  Fetch
	dir    string        // "" keeps nothing on disk
	slots  chan struct{} // pictures made at once
	landed chan struct{}

	mu      sync.Mutex
	made    *rows.Cache[key, Pic]
	pending map[key]bool
	writes  strings.Builder
}

// New makes a Set fetching with fetch and keeping PNGs in dir.
func New(ctx context.Context, fetch Fetch, dir string) *Set {
	return &Set{
		ctx: ctx, fetch: fetch, dir: dir,
		slots:   make(chan struct{}, 2),
		landed:  make(chan struct{}, 1),
		made:    rows.NewCache[key, Pic](keep),
		pending: map[key]bool{},
	}
}

// Dir is where pictures are kept on disk.
func Dir() string {
	d, _ := os.UserCacheDir()
	return filepath.Join(d, "loafer", "images")
}

// Get is url's picture fitted to at most cols by rows cells, and whether
// it's been made (or failed to be). It never waits: the first ask starts
// it, and Landed says when it's done.
func (s *Set) Get(url string, cols, rows int) (Pic, bool) {
	cw, ch := termimg.Cell()
	k := key{url, cols, rows, cw, ch}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.made.Get(k); ok {
		return p, true
	}
	if !s.pending[k] {
		s.pending[k] = true
		go s.make(k)
	}
	return Pic{}, false
}

// Landed has a value once a picture's been made since it was last taken.
func (s *Set) Landed() <-chan struct{} { return s.landed }

// Writes takes the Kitty transmissions made since it was last called, to
// be written to the terminal raw; "" when there are none.
func (s *Set) Writes() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.writes.String()
	s.writes.Reset()
	return w
}

func (s *Set) make(k key) {
	s.slots <- struct{}{}
	p, seq := s.draw(k)
	<-s.slots
	s.mu.Lock()
	delete(s.pending, k)
	// shortcut: a failure is kept like a picture, so one that failed offline
	// stays initials until it's evicted; retry on reconnect if that bites.
	s.made.Put(k, p, 1)
	s.made.Evict(nil)
	s.writes.WriteString(seq)
	s.mu.Unlock()
	select {
	case s.landed <- struct{}{}:
	default:
	}
}

// draw makes k's picture and the escape sequence that sends it.
func (s *Set) draw(k key) (Pic, string) {
	img, err := s.load(k)
	if err != nil {
		slog.Debug("image", "err", err.Error())
		return Pic{}, ""
	}
	b := img.Bounds()
	cols, rows := termimg.Fit(b.Dx(), b.Dy(), k.cols, k.rows, k.cw, k.ch)
	if cols == 0 {
		return Pic{}, ""
	}
	id := termimg.ID([]byte(k.url), cols, rows)
	seq, err := termimg.Transmit(id, img, cols, rows)
	if err != nil {
		return Pic{}, ""
	}
	cells := termimg.Placeholders(id, cols, rows)
	for i, c := range cells {
		cells[i] = ansi.Strip(c)
	}
	return Pic{ID: id, Cols: cols, Rows: rows, Cells: cells}, seq
}

// load is k's picture already fitted: from disk if it's there, else
// fetched, fitted and put there.
func (s *Set) load(k key) (image.Image, error) {
	path := s.path(k)
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			if img, err := png.Decode(bytes.NewReader(b)); err == nil {
				return img, nil
			}
		}
	}
	data, err := s.fetch(s.ctx, k.url)
	if err != nil {
		return nil, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width*cfg.Height > 64<<20 {
		return nil, errors.New("image too big")
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	cols, rows := termimg.Fit(cfg.Width, cfg.Height, k.cols, k.rows, k.cw, k.ch)
	if cols == 0 {
		return nil, errors.New("no room")
	}
	out := image.NewNRGBA(image.Rect(0, 0, min(cfg.Width, cols*k.cw), min(cfg.Height, rows*k.ch)))
	draw.CatmullRom.Scale(out, out.Bounds(), src, src.Bounds(), draw.Src, nil)
	if path != "" {
		// shortcut: the disk cache is never pruned; avatars are a few KB each.
		var b bytes.Buffer
		if png.Encode(&b, out) == nil && os.MkdirAll(s.dir, 0o700) == nil {
			if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
				slog.Warn("image cache", "err", err.Error())
			}
		}
	}
	return out, nil
}

// path is where k's fitted PNG is kept: named by a hash of its URL and
// size, so the URL itself isn't on disk.
func (s *Set) path(k key) string {
	if s.dir == "" {
		return ""
	}
	h := sha256.Sum256([]byte(k.url + " " + strconv.Itoa(k.cols) + "x" + strconv.Itoa(k.rows) + " " + strconv.Itoa(k.cw) + "x" + strconv.Itoa(k.ch)))
	return filepath.Join(s.dir, hex.EncodeToString(h[:16])+".png")
}
