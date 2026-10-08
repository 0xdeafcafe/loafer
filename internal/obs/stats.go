package obs

import (
	"runtime"
	"runtime/metrics"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// --- frames ---

var frames struct {
	sync.Mutex
	n, skipped int64
	last, max  time.Duration
	total      time.Duration
}

// Frame records one drawn frame and how long it took.
func Frame(took time.Duration) {
	frames.Lock()
	frames.n++
	frames.last = took
	frames.total += took
	frames.max = max(frames.max, took)
	frames.Unlock()
	if took > 16*time.Millisecond {
		slowFrames.Add(1)
	}
}

// SkippedFrame records a frame not drawn because nothing visible changed.
func SkippedFrame() {
	frames.Lock()
	frames.skipped++
	frames.Unlock()
}

var slowFrames atomic.Int64

// --- named numbers ---

var gauges sync.Map // string -> *atomic.Int64

// Gauge is a named number the debug strip shows, like "api.inflight" or
// "ws.lag_ms". Get it once and keep it; Add and Store are atomic.
func Gauge(name string) *atomic.Int64 {
	if g, ok := gauges.Load(name); ok {
		return g.(*atomic.Int64)
	}
	g, _ := gauges.LoadOrStore(name, new(atomic.Int64))
	return g.(*atomic.Int64)
}

// --- snapshot ---

// Snapshot is what the debug strip shows.
type Snapshot struct {
	Heap, RSS           uint64  // bytes
	CPU                 float64 // percent of one core since the last snapshot
	Goroutines          int
	GCs                 uint64
	Frames, Skipped     int64
	SlowFrames          int64
	LastFrame, MaxFrame time.Duration
	AvgFrame            time.Duration
	Dropped             int64
	Gauges              map[string]int64
}

var cpu struct {
	sync.Mutex
	at   time.Time
	used time.Duration
}

var samples = []metrics.Sample{
	{Name: "/memory/classes/heap/objects:bytes"},
	{Name: "/gc/cycles/total:gc-cycles"},
}

// Take reads the numbers. It costs a few microseconds and allocates a
// little, so the strip calls it once a second while it's open, never per
// frame.
func Take() Snapshot {
	metrics.Read(samples)
	s := Snapshot{
		Heap:       samples[0].Value.Uint64(),
		GCs:        samples[1].Value.Uint64(),
		Goroutines: runtime.NumGoroutine(),
		Dropped:    Dropped(),
		SlowFrames: slowFrames.Load(),
		Gauges:     map[string]int64{},
	}
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) == nil {
		s.RSS = maxRSS(ru)
		used := time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
		now := time.Now()
		cpu.Lock()
		if !cpu.at.IsZero() {
			s.CPU = 100 * float64(used-cpu.used) / float64(now.Sub(cpu.at))
		}
		cpu.at, cpu.used = now, used
		cpu.Unlock()
	}
	frames.Lock()
	s.Frames, s.Skipped = frames.n, frames.skipped
	s.LastFrame, s.MaxFrame = frames.last, frames.max
	if frames.n > 0 {
		s.AvgFrame = frames.total / time.Duration(frames.n)
	}
	frames.Unlock()
	gauges.Range(func(k, v any) bool {
		s.Gauges[k.(string)] = v.(*atomic.Int64).Load()
		return true
	})
	return s
}
