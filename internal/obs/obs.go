// Package obs is what loafer tells you about itself: an event log (a ring
// in memory and a rotating file), trace switches for the noisy kinds, a few
// live numbers for the debug strip, and profiles on demand. Logging never
// waits on the disk: lines are formatted where they're logged and written
// by one goroutine, and dropped (and counted) if it falls behind.
package obs

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Dir is where logs and profiles go.
func Dir() string {
	if d := os.Getenv("LOAFER_LOGS"); d != "" {
		return d
	}
	if h, err := os.UserHomeDir(); err == nil && runtime.GOOS == "darwin" {
		return filepath.Join(h, "Library", "Logs", "loafer")
	}
	d, _ := os.UserCacheDir()
	return filepath.Join(d, "loafer", "logs")
}

// --- trace switches ---

// Kind is a noisy kind of event, logged in full only while traced.
type Kind uint32

const (
	API Kind = 1 << iota
	WS
	Render
	Store
)

var kindNames = map[string]Kind{"api": API, "ws": WS, "render": Render, "store": Store}

var tracing atomic.Uint32

// Trace turns tracing on for a comma-separated list of kinds ("api,ws",
// or "all"), as LOAFER_TRACE and --trace give it.
func Trace(list string) {
	var k Kind
	for n := range strings.SplitSeq(list, ",") {
		n = strings.TrimSpace(strings.ToLower(n))
		if n == "all" {
			k = ^Kind(0)
		}
		k |= kindNames[n]
	}
	tracing.Store(uint32(k))
}

// Tracing says whether k is traced. It's one atomic load, so hot paths
// can ask before building anything to log.
func Tracing(k Kind) bool { return Kind(tracing.Load())&k != 0 }

// --- redaction ---

// Slack's tokens and the d cookie never reach a log line, a report or a
// trace: every line passes through Redact before it's kept.
var secret = regexp.MustCompile(`xox[a-z]-[A-Za-z0-9%._-]+|(\bd=)[A-Za-z0-9%/+=._-]{20,}|("token"\s*:\s*")[^"]+`)

func Redact(s string) string {
	return secret.ReplaceAllStringFunc(s, func(m string) string {
		switch {
		case strings.HasPrefix(m, "d="):
			return "d=REDACTED"
		case strings.HasPrefix(m, `"token"`):
			return m[:strings.IndexByte(m, ':')] + `:"REDACTED`
		}
		return m[:5] + "REDACTED"
	})
}

// --- the ring ---

// Entry is one logged event, as kept for the event viewer.
type Entry struct {
	T     time.Time
	Level slog.Level
	Msg   string
	Line  string // the whole event as JSON, redacted
}

const ringSize = 4096

var ring struct {
	sync.Mutex
	e    [ringSize]Entry
	n    uint64 // events ever kept; e[(n-1)%ringSize] is the newest
	tail chan Entry
}

// Recent returns up to max of the newest events, oldest first, and the
// count ever kept so a viewer can tell what's new since it last looked.
func Recent(max int) ([]Entry, uint64) {
	ring.Lock()
	defer ring.Unlock()
	have := int(min(ring.n, ringSize))
	max = min(max, have)
	out := make([]Entry, max)
	for i := range out {
		out[i] = ring.e[(ring.n-uint64(max)+uint64(i))%ringSize]
	}
	return out, ring.n
}

func keep(e Entry) {
	ring.Lock()
	ring.e[ring.n%ringSize] = e
	ring.n++
	ring.Unlock()
}

// --- the handler ---

var dropped atomic.Int64

type handler struct {
	slog.Handler
	buf *bytes.Buffer
	mu  *sync.Mutex
}

// Handle formats the record once (under a lock, into one shared buffer)
// and hands the line to the ring and the file writer.
func (h handler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	h.buf.Reset()
	err := h.Handler.Handle(ctx, r)
	line := Redact(strings.TrimSuffix(h.buf.String(), "\n"))
	h.mu.Unlock()
	if err != nil {
		return err
	}
	e := Entry{T: r.Time, Level: r.Level, Msg: r.Message, Line: line}
	keep(e)
	select {
	case ring.tail <- e:
	default:
		dropped.Add(1)
	}
	return nil
}

func (h handler) WithAttrs(as []slog.Attr) slog.Handler {
	return handler{h.Handler.WithAttrs(as), h.buf, h.mu}
}

func (h handler) WithGroup(n string) slog.Handler {
	return handler{h.Handler.WithGroup(n), h.buf, h.mu}
}

// Start makes slog's default logger loafer's: events go to the ring and,
// unless file is empty, to file (rotated at 10 MB, 5 kept). Level is Debug
// when anything is traced. It returns a func that flushes and closes.
func Start(file string) (stop func()) {
	if t := os.Getenv("LOAFER_TRACE"); t != "" {
		Trace(t)
	}
	level := slog.LevelInfo
	if tracing.Load() != 0 {
		level = slog.LevelDebug
	}
	buf := new(bytes.Buffer)
	h := handler{slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: level}), buf, new(sync.Mutex)}
	ring.tail = make(chan Entry, 1024)
	slog.SetDefault(slog.New(h))

	var w io.WriteCloser = nopCloser{io.Discard}
	if file != "" {
		w = &rotator{path: file, max: 10 << 20, keep: 5}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range ring.tail {
			io.WriteString(w, e.Line+"\n")
		}
		w.Close()
	}()
	return func() {
		close(ring.tail)
		<-done
	}
}

// Dropped is how many lines the file writer missed because it fell behind.
func Dropped() int64 { return dropped.Load() }

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

// rotator appends to path, moving it to path.1 (and path.1 to path.2, and
// so on, keep deep) when it passes max bytes. It's only written from the
// writer goroutine.
type rotator struct {
	path      string
	max, size int64
	keep      int
	f         *os.File
}

func (r *rotator) Write(p []byte) (int, error) {
	if r.f == nil {
		if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
			return 0, err
		}
		f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return 0, err
		}
		fi, _ := f.Stat()
		r.f, r.size = f, fi.Size()
	}
	if r.size+int64(len(p)) > r.max && r.size > 0 {
		r.f.Close()
		r.f = nil
		for i := r.keep - 1; i > 0; i-- {
			os.Rename(r.name(i), r.name(i+1))
		}
		os.Rename(r.path, r.name(1))
		return r.Write(p)
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotator) name(i int) string { return r.path + "." + string(rune('0'+i)) }

func (r *rotator) Close() error {
	if r.f == nil {
		return nil
	}
	return r.f.Close()
}
