package ui

import (
	"fmt"
	"log/slog"
	"time"
)

// stallAt is the longest the UI goroutine may spend on one message or one
// frame before it's logged (docs/design.md, the stall watchdog).
const stallAt = 75 * time.Millisecond

// stalled logs a message handled, or a frame drawn when msg is nil, that
// took longer than stallAt since began, with what was on screen.
func (m *Model) stalled(began time.Time, msg any) {
	d := time.Since(began)
	if d <= stallAt {
		return
	}
	what := "frame"
	if msg != nil {
		what = fmt.Sprintf("%T", msg)
	}
	slog.Warn("stall", "what", what, "ms", d.Milliseconds(), "tab", m.tabs.on, "conv", m.open, "thread", m.th.ts != "", "size", fmt.Sprintf("%dx%d", m.w, m.h), "bytes", m.frame.Len())
}
