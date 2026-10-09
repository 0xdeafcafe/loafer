package images

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xdeafcafe/photon/termimg"
)

// server serves a 72×72 PNG at every path but /missing, counting hits;
// each waits for gate when it's set.
func server(t *testing.T, gate chan struct{}) (*httptest.Server, *atomic.Int64) {
	img := image.NewNRGBA(image.Rect(0, 0, 72, 72))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.Set(0, 0, color.Black)
	var b bytes.Buffer
	png.Encode(&b, img)
	hits := new(atomic.Int64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if gate != nil {
			<-gate
		}
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		w.Write(b.Bytes())
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func get(ctx context.Context, url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, os.ErrNotExist
	}
	return io.ReadAll(resp.Body)
}

// wait asks for url until it's made.
func wait(t *testing.T, s *Set, url string, cols, rows int) Pic {
	t.Helper()
	for range 200 {
		if p, ok := s.Get(url, cols, rows); ok {
			return p
		}
		select {
		case <-s.Landed():
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatalf("%s never landed", url)
	return Pic{}
}

func TestAvatarMadeAndKeptOnDisk(t *testing.T) {
	termimg.SetCell(10, 20)
	srv, hits := server(t, nil)
	dir := t.TempDir()
	s := New(context.Background(), get, dir)
	p := wait(t, s, srv.URL+"/a.png", 2, 1)
	if p.Cols != 2 || p.Rows != 1 || len(p.Cells) != 1 || p.ID == 0 {
		t.Fatalf("%+v", p)
	}
	if strings.Contains(p.Cells[0], "\x1b") {
		t.Fatalf("cells carry colour: %q", p.Cells[0])
	}
	if w := s.Writes(); !strings.HasPrefix(w, "\x1b_G") || s.Writes() != "" {
		t.Fatalf("writes: %.20q", w)
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatalf("%d files on disk", len(files))
	}
	cfg, _ := png.DecodeConfig(must(os.Open(dir + "/" + files[0].Name())))
	if cfg.Width != 20 || cfg.Height != 20 {
		t.Fatalf("kept at %dx%d, want the cells' 20x20", cfg.Width, cfg.Height)
	}

	// A new Set (a new run) finds it on disk, without asking the server.
	again := wait(t, New(context.Background(), get, dir), srv.URL+"/a.png", 2, 1)
	if hits.Load() != 1 || again.ID != p.ID {
		t.Fatalf("hits %d, id %d vs %d", hits.Load(), again.ID, p.ID)
	}
}

func must(f *os.File, err error) *os.File {
	if err != nil {
		panic(err)
	}
	return f
}

// Asked for many times while it's on its way, a picture is fetched once.
func TestDedupe(t *testing.T) {
	termimg.SetCell(10, 20)
	gate := make(chan struct{})
	srv, hits := server(t, gate)
	s := New(context.Background(), get, "")
	for range 50 {
		if _, ok := s.Get(srv.URL+"/a.png", 2, 1); ok {
			t.Fatal("made before it was fetched")
		}
	}
	close(gate)
	wait(t, s, srv.URL+"/a.png", 2, 1)
	if n := hits.Load(); n != 1 {
		t.Fatalf("fetched %d times", n)
	}
}

// Only keep pictures stay in memory; the oldest go first.
func TestLRU(t *testing.T) {
	termimg.SetCell(10, 20)
	srv, _ := server(t, nil)
	s := New(context.Background(), get, "")
	for i := range keep + 20 {
		wait(t, s, srv.URL+"/"+strconv.Itoa(i), 2, 1)
	}
	if n := s.made.Len(); n != keep {
		t.Fatalf("%d kept", n)
	}
	if _, ok := s.made.Get(key{srv.URL + "/0", 2, 1, 10, 20}); ok {
		t.Fatal("the oldest is still kept")
	}
	if _, ok := s.made.Get(key{srv.URL + "/" + strconv.Itoa(keep+19), 2, 1, 10, 20}); !ok {
		t.Fatal("the newest went")
	}
}

// One that can't be had is made as nothing, once, so it falls back.
func TestFailure(t *testing.T) {
	termimg.SetCell(10, 20)
	srv, hits := server(t, nil)
	s := New(context.Background(), get, t.TempDir())
	if p := wait(t, s, srv.URL+"/missing", 2, 1); p.OK() {
		t.Fatalf("%+v", p)
	}
	s.Get(srv.URL+"/missing", 2, 1)
	if hits.Load() != 1 || s.Writes() != "" {
		t.Fatalf("hits %d", hits.Load())
	}
}

// A picture keeps its shape: a wide one fills the width, not the rows.
func TestFit(t *testing.T) {
	termimg.SetCell(10, 20)
	srv, _ := server(t, nil)
	s := New(context.Background(), get, "")
	p := wait(t, s, srv.URL+"/b.png", 40, 8) // 72×72 is its own size: 7 cells by 4 rows
	if p.Cols != 7 || p.Rows != 4 || len(p.Cells) != 4 {
		t.Fatalf("%dx%d", p.Cols, p.Rows)
	}
}
