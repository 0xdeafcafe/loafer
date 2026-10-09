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
	if m := s.Misses(); m < 50 {
		t.Fatalf("%d misses, want each ask before it landed", m)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("fetched %d times", n)
	}
}

// A burst of transmissions goes out a writeMost at a time, never split,
// with Landed saying there's more.
func TestWritesPaced(t *testing.T) {
	s := New(context.Background(), get, "")
	big := strings.Repeat("x", writeMost/3)
	s.writes = []string{big, big, big, big, big + big + big + big}
	for i, want := range []int{3, 1, 4} {
		if n := len(s.Writes()); n != want*len(big) {
			t.Fatalf("write %d is %d bytes, want %d", i, n, want*len(big))
		}
		if more := len(s.writes) > 0; more != (len(s.Landed()) == 1) {
			t.Fatalf("write %d: more left %v, said %v", i, more, !more)
		}
		select {
		case <-s.Landed():
		default:
		}
	}
	if s.Writes() != "" {
		t.Fatal("writes after the last")
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

// One that failed is tried once more when it's asked for after a while,
// and then left be.
func TestRetryOnce(t *testing.T) {
	termimg.SetCell(10, 20)
	was := retryAfter
	retryAfter = 0
	t.Cleanup(func() { retryAfter = was })
	srv, hits := server(t, nil)
	s := New(context.Background(), get, "")
	busy := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.pending) > 0
	}
	wait(t, s, srv.URL+"/missing", 2, 1)
	s.Get(srv.URL+"/missing", 2, 1) // the retry
	for busy() {
		select {
		case <-s.Landed():
		case <-time.After(5 * time.Second):
			t.Fatal("the retry never landed")
		}
	}
	for range 3 {
		s.Get(srv.URL+"/missing", 2, 1)
	}
	if n := hits.Load(); n != 2 || busy() {
		t.Fatalf("fetched %d times, want 2", n)
	}
}

// The disk cache is pruned to its cap, the oldest first.
func TestPrune(t *testing.T) {
	dir := t.TempDir()
	at := time.Now().Add(-time.Hour)
	for i, name := range []string{"old.png", "mid.png", "new.png"} {
		p := dir + "/" + name
		if err := os.WriteFile(p, make([]byte, 100), 0o600); err != nil {
			t.Fatal(err)
		}
		when := at.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(p, when, when); err != nil {
			t.Fatal(err)
		}
	}
	prune(dir, 200)
	es, _ := os.ReadDir(dir)
	if len(es) != 2 || es[0].Name() != "mid.png" || es[1].Name() != "new.png" {
		t.Fatalf("left %v", es)
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
