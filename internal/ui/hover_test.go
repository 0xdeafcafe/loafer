package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// rowOf is the screen row whose text has s.
func rowOf(t *testing.T, m *Model, s string) int {
	t.Helper()
	for y, l := range plainFrame(m.render()) {
		if strings.Contains(l, s) {
			return y
		}
	}
	t.Fatalf("no row has %q", s)
	return 0
}

// The pointer over a message lifts it and offers its actions, drawing no
// message again; moving within it changes nothing; blur lets it go.
func TestHover(t *testing.T) {
	m := fixture(t)
	m.render()
	x := m.hov.x + 10
	y := rowOf(t, m, "message 36 ")
	drawn := m.hov.n

	m.Update(tea.MouseMotionMsg{X: x, Y: y})
	if m.hov.ts == "" {
		t.Fatal("no hover over a message")
	}
	text := strings.Join(plainFrame(m.render()), "\n")
	if !strings.Contains(text, "☺ react · ↩ reply · ⋯") {
		t.Fatalf("no actions on the hovered message:\n%s", text)
	}
	was := m.hov.ts
	if m.hoverAt(x+5, y) {
		t.Fatal("moving within the message should change nothing")
	}
	other := rowOf(t, m, "message 34 ")
	m.Update(tea.MouseMotionMsg{X: x, Y: other})
	m.render()
	if m.hov.ts == was || m.hov.ts == "" {
		t.Fatalf("the hover didn't follow: %q", m.hov.ts)
	}
	if n := m.hov.n - drawn; n > 2 {
		t.Fatalf("hovering drew %d messages again, want at most 2", n)
	}

	m.Update(tea.MouseMotionMsg{X: 0, Y: y}) // over the sidebar
	if m.hov.ts != "" {
		t.Fatal("the hover stayed off the list")
	}
	m.Update(tea.MouseMotionMsg{X: x, Y: y})
	m.Update(tea.BlurMsg{})
	if m.hov.ts != "" {
		t.Fatal("the hover stayed through a blur")
	}
}

// A click on a hovered message's react opens the picker on it.
func TestHoverClick(t *testing.T) {
	m := fixture(t)
	m.render()
	y := rowOf(t, m, "message 36 ")
	m.Update(tea.MouseMotionMsg{X: m.hov.x + 10, Y: y})
	frame := plainFrame(m.render())
	head := y
	for head > 0 && !strings.Contains(frame[head], "☺ react") {
		head--
	}
	x := strings.Index(frame[head], "☺ react")
	x = len([]rune(frame[head][:x])) // cells, these being one wide
	m.Update(tea.MouseClickMsg{X: x, Y: head, Button: tea.MouseLeft})
	if m.sel == "" || !m.emo.pick.on {
		t.Fatalf("the click picked %q, picker %v", m.sel, m.emo.pick.on)
	}
}

func BenchmarkHover(b *testing.B) {
	m := fixture(b)
	m.render()
	y0 := m.hov.y + m.hov.lead
	for i := 0; b.Loop(); i++ {
		m.Update(tea.MouseMotionMsg{X: m.hov.x + 10, Y: y0 + i%20})
		m.View()
	}
}
