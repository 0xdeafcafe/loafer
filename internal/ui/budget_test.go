package ui

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/obs"
	"github.com/0xdeafcafe/loafer/internal/slacktest"
	"github.com/0xdeafcafe/loafer/internal/store"
)

// TestBudget runs loafer against the big workspace (slacktest.NewBig) as
// bubbletea would, and measures it against docs/design.md's budgets:
// starting, frames in the ways it's used, memory, goroutines and what
// wakes it while idle. It prints a table (go test ./internal/ui -run
// TestBudget -v) and fails a budget missed, except under -race, which
// slows everything down. docs/perf.md has the numbers.
func TestBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("the budget takes a while")
	}
	prev := slog.Default()
	obs.Start("") // logged as loafer logs, into the ring
	t.Cleanup(func() { slog.SetDefault(prev) })

	srv := slacktest.NewBig()
	t.Cleanup(srv.Close)
	runtime.GC()
	base, baseG := heap(), runtime.NumGoroutine()
	path := filepath.Join(t.TempDir(), "state.json")
	var tab budget

	// A cold start fills the cache, as a day's use would.
	cold, stop := context.WithCancel(context.Background())
	r := newRig(t, cold, srv, store.New())
	began := time.Now()
	r.run(r.m.Init())
	r.untilLive()
	tab.add("cold start to live", ms(time.Since(began)), "", true)
	r.browse(20)
	if err := r.m.st.Save(path); err != nil {
		t.Fatal(err)
	}
	stop()
	r = nil

	// The start that counts: from the cache.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	began = time.Now()
	st := store.New()
	if err := st.Load(path); err != nil {
		t.Fatal(err)
	}
	go st.WriteBehind(ctx, path)
	r = newRig(t, ctx, srv, st)
	cmd := r.m.Init()
	r.m.View()
	first := time.Since(began)
	tab.add("start to first frame, from the cache", ms(first), "150ms", first < 150*time.Millisecond)
	if !strings.Contains(r.m.gate.Last(), "firehose") {
		t.Errorf("the first frame isn't from the cache:\n%s", r.m.gate.Last())
	}
	r.run(cmd)
	r.untilLive()
	live := time.Since(began)
	tab.add("start to live", ms(live), "1.5s", live < 1500*time.Millisecond)
	r.settle()
	tab.add("heap after boot", mb(heap()-base), "", true)
	tab.add("goroutines", fmt.Sprint(runtime.NumGoroutine()-baseG), "", true)

	// Idle: frames drawn again over nothing new, then nothing at all.
	r.reset()
	for range 300 {
		r.draw()
	}
	tab.frames("frames, idle", r)
	wakes, msgs, cpu := r.idle(5 * time.Second)
	tab.add("idle 5s: wakes, messages, cpu", fmt.Sprintf("%d, %d, %.2f%%", wakes, len(msgs), cpu), "0, 0, 0.2%",
		len(msgs) == 0 && cpu < 0.2)
	if len(msgs) > 0 {
		t.Logf("woken while idle by %v", msgs)
	}

	// The sidebar, top to bottom.
	r.reset()
	r.m.setFocus(onSide)
	r.m.sideAt = 0
	for range len(r.m.side) {
		r.step(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	tab.frames(fmt.Sprintf("frames, sidebar (%d rows)", len(r.m.side)), r)

	// ctrl+k, typing and taking it back.
	r.reset()
	r.step(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	for _, q := range []string{"eng-pay", "ada", "fire"} {
		for _, c := range q {
			r.step(tea.KeyPressMsg{Code: c, Text: string(c)})
		}
		for range q {
			r.step(tea.KeyPressMsg{Code: tea.KeyBackspace})
		}
	}
	r.step(tea.KeyPressMsg{Code: tea.KeyEscape})
	tab.frames(fmt.Sprintf("frames, ctrl+k (%d places)", len(r.m.sideConvs())), r)

	// Twenty conversations, each held from the cold run.
	r.reset()
	switches := r.browse(20)
	slices.Sort(switches)
	tab.add("switching, p50 / p99 (to frame)", ms(pct(switches, 50))+" / "+ms(pct(switches, 99)), "5ms", pct(switches, 99) < 5*time.Millisecond)
	r.settle()
	tab.add("heap after 20 conversations", mb(heap()-base), "45MB", heap()-base < 45<<20)

	// #firehose, scrolled back a few thousand messages by the wheel.
	r.run(r.m.visit(slacktest.Big))
	r.until("#firehose", 5*time.Second, func() bool { return r.held(slacktest.Big) })
	r.m.setFocus(onMsgs)
	r.reset()
	for burst := range 40 {
		for range 10 {
			r.step(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
		}
		r.pump(20 * time.Millisecond) // the gate's tick draws where the burst ended
		if burst%2 == 0 {
			r.step(tea.KeyPressMsg{Code: 'g', Text: "g"}) // the oldest held, which fetches older
			r.until("older messages", 5*time.Second, func() bool { return !r.m.fetching })
		}
	}
	tab.frames(fmt.Sprintf("frames, scrolling #firehose (%d held)", r.window(slacktest.Big)), r)

	// The stream: 50 events a second across the workspace.
	r.step(tea.KeyPressMsg{Code: 'G', Text: "G"})
	r.reset()
	streaming, quiet := context.WithCancel(ctx)
	go srv.Stream(streaming, 50)
	r.pump(5 * time.Second)
	quiet()
	tab.frames(fmt.Sprintf("frames, the stream (%d in 5s)", len(r.frames)), r)

	fmt.Fprint(os.Stdout, tab.String())
	if !raceOn {
		for _, l := range tab {
			if !l.ok {
				t.Errorf("over budget: %s: %s, budget %s", l.what, l.got, l.budget)
			}
		}
	}
}

// --- the rig ---

// rig drives a Model as e2e does, timing each Update and each frame
// drawn. Notifications are dropped on the way: a test never shows one.
type rig struct {
	*e2e
	tb           testing.TB
	frames, keys []time.Duration
	allocs       []uint64
}

func newRig(tb testing.TB, ctx context.Context, srv *slacktest.Server, st *store.Store) *rig {
	d := &e2e{srv: srv, ctx: ctx, msgs: make(chan tea.Msg, 256)}
	d.m = New(ctx, st, srv.Client())
	d.m.w, d.m.h = 200, 60
	return &rig{e2e: d, tb: tb}
}

// bigRig is a rig booted on the big workspace, settled, with #firehose
// open, for benchmarks.
func bigRig(b *testing.B) *rig {
	srv := slacktest.NewBig()
	ctx, cancel := context.WithCancel(context.Background())
	b.Cleanup(func() { cancel(); srv.Close() })
	r := newRig(b, ctx, srv, store.New())
	r.run(r.m.Init())
	r.untilLive()
	r.run(r.m.visit(slacktest.Big))
	r.until("#firehose", 5*time.Second, func() bool { return r.held(slacktest.Big) })
	r.settle()
	return r
}

func BenchmarkBigFrame(b *testing.B) {
	r := bigRig(b)
	for b.Loop() {
		r.m.View()
	}
}

func BenchmarkBigJumpKey(b *testing.B) {
	r := bigRig(b)
	r.m.openJump()
	for b.Loop() {
		r.m.jumpKey(tea.KeyPressMsg{Code: 'e', Text: "e"})
		r.m.jumpKey(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
}

func BenchmarkBigMention(b *testing.B) {
	r := bigRig(b)
	r.m.setFocus(onCompose)
	r.m.insert("@ad")
	for b.Loop() {
		r.m.refreshPop()
	}
}

func BenchmarkBigSidebar(b *testing.B) {
	r := bigRig(b)
	for b.Loop() {
		r.m.st.Read(r.m.buildSide)
	}
}

func (r *rig) reset() { r.frames, r.keys, r.allocs = nil, nil, nil }

func (r *rig) step(msg tea.Msg) {
	if _, ok := msg.(noteMsg); ok {
		return
	}
	began := time.Now()
	_, cmd := r.m.Update(msg)
	r.keys = append(r.keys, time.Since(began))
	r.run(cmd)
	if same, _ := r.m.gate.Held(); same {
		r.m.View() // the last frame again, which costs nothing
		return
	}
	r.draw()
}

// draw times one frame drawn.
func (r *rig) draw() {
	a := allocs()
	began := time.Now()
	r.m.View()
	r.frames = append(r.frames, time.Since(began))
	r.allocs = append(r.allocs, allocs()-a)
}

func (r *rig) until(what string, within time.Duration, ok func() bool) {
	r.tb.Helper()
	deadline := time.After(within)
	for !ok() {
		select {
		case msg := <-r.msgs:
			r.step(msg)
		case <-deadline:
			r.tb.Fatalf("waited %s for %s", within, what)
		}
	}
}

// pump handles what comes for d.
func (r *rig) pump(d time.Duration) {
	end := time.After(d)
	for {
		select {
		case msg := <-r.msgs:
			r.step(msg)
		case <-end:
			return
		}
	}
}

// settle handles what comes until nothing has for half a second.
func (r *rig) settle() {
	for {
		select {
		case msg := <-r.msgs:
			r.step(msg)
		case <-time.After(500 * time.Millisecond):
			runtime.GC()
			runtime.GC()
			return
		}
	}
}

func (r *rig) untilLive() {
	r.until("boot and the socket", 30*time.Second, func() bool { return r.m.live == "live" && r.m.open != "" })
}

func (r *rig) held(conv string) bool { return r.window(conv) > 0 }

func (r *rig) window(conv string) (n int) {
	r.m.st.Read(func(v store.View) {
		if w := v.Window(conv); w != nil {
			n = len(w.Msgs)
		}
	})
	return n
}

// browse opens the first n conversations in the sidebar from it, as
// ↓ and enter would, and says how long each took to its frame.
func (r *rig) browse(n int) []time.Duration {
	var took []time.Duration
	for i := 0; i < len(r.m.side) && len(took) < n; i++ {
		id := r.m.side[i].conv
		if id == "" || id == r.m.open {
			continue
		}
		r.m.setFocus(onSide)
		r.m.sideAt = i
		began := time.Now()
		r.step(tea.KeyPressMsg{Code: tea.KeyEnter})
		took = append(took, time.Since(began))
		r.until(id, 5*time.Second, func() bool { return r.held(id) })
	}
	return took
}

// idle watches for d with nothing arriving: the goroutines the runtime
// woke (it samples one in eight), the messages that reached Update, and
// the CPU used, as a share of one core.
func (r *rig) idle(d time.Duration) (wakes uint64, msgs []string, cpu float64) {
	r.settle()
	w, c := schedWakes(), cpuTime()
	end := time.After(d)
	for {
		select {
		case msg := <-r.msgs:
			msgs = append(msgs, fmt.Sprintf("%T", msg))
			r.step(msg)
		case <-end:
			return schedWakes() - w, msgs, 100 * float64(cpuTime()-c) / float64(d)
		}
	}
}

// --- the numbers ---

var samples = []metrics.Sample{{Name: "/gc/heap/allocs:objects"}, {Name: "/sched/latencies:seconds"}}

func allocs() uint64 {
	metrics.Read(samples[:1])
	return samples[0].Value.Uint64()
}

func schedWakes() (n uint64) {
	metrics.Read(samples[1:])
	for _, c := range samples[1].Value.Float64Histogram().Counts {
		n += c
	}
	return n
}

func heap() uint64 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

func cpuTime() time.Duration {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

func pct(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[min(len(sorted)-1, len(sorted)*p/100)]
}

func ms(d time.Duration) string {
	if d < 10*time.Millisecond {
		return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000)
	}
	return d.Round(time.Millisecond).String()
}

func mb(b uint64) string { return fmt.Sprintf("%.1fMB", float64(b)/(1<<20)) }

type budgetLine struct {
	what, got, budget string
	ok                bool
}

type budget []budgetLine

func (b *budget) add(what, got, limit string, ok bool) {
	*b = append(*b, budgetLine{what, got, limit, ok})
}

// frames adds r's frames: p50 and p99 drawing, the median frame's
// allocations, and p99 handling a message. The budget is the design's
// warm frame: 1.5ms and 50 allocations.
func (b *budget) frames(what string, r *rig) {
	f, k, a := slices.Sorted(slices.Values(r.frames)), slices.Sorted(slices.Values(r.keys)), slices.Sorted(slices.Values(r.allocs))
	var al uint64
	if len(a) > 0 {
		al = a[len(a)/2]
	}
	got := fmt.Sprintf("%s / %s, %d allocs, update p99 %s", ms(pct(f, 50)), ms(pct(f, 99)), al, ms(pct(k, 99)))
	b.add(what, got, "1.5ms, 50", pct(f, 50) < 1500*time.Microsecond && al <= 50)
}

func (b budget) String() string {
	var s strings.Builder
	w := 0
	for _, l := range b {
		w = max(w, len(l.what))
	}
	fmt.Fprintf(&s, "\n%-*s  %-44s  %s\n", w, "what", "got (frames: p50 / p99, median allocs)", "budget")
	for _, l := range b {
		mark := ""
		if !l.ok {
			mark = "  ✗ over"
		}
		fmt.Fprintf(&s, "%-*s  %-44s  %s%s\n", w, l.what, l.got, l.budget, mark)
	}
	return s.String()
}
