package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

var (
	down  = tea.KeyPressMsg{Code: tea.KeyDown}
	right = tea.KeyPressMsg{Code: tea.KeyRight}
)

// reacted is the names Slack has been asked to add, in order.
func (d *e2e) reacted() (names []string) {
	for _, c := range d.srv.Calls() {
		if c.Method == "reactions.add" {
			names = append(names, c.Form.Get("name"))
		}
	}
	return names
}

// clickCell clicks the picker's cell col on grid line li of those shown.
func (d *e2e) clickCell(li, col int) {
	p := &d.m.emo.pick
	d.update(tea.MouseClickMsg{X: p.gx + col*pickCellW + 1, Y: p.gy + li, Button: tea.MouseLeft})
}

// The picker is a grid: what you use, then the categories, then the
// workspace's own. The arrows move in it and enter reacts; a click reacts
// with what it's on, a custom emoji's picture too; the wheel scrolls.
func TestE2EReactGrid(t *testing.T) {
	open := make(chan struct{})
	close(open)
	pic := withPics(t, open)
	d := newE2E(t)
	d.m.st.ApplyEmoji(map[string]string{"loaf": pic.URL + "/loaf.png"})
	d.oldestInDev()

	d.press(r('r'))
	for _, want := range []string{"☺ React", "search emoji", "Frequently used", "Smileys & emotion", ":+1:"} {
		if !d.has(want) {
			t.Errorf("the picker lacks %q", want)
		}
	}
	if t.Failed() {
		t.Fatal("\n" + d.text)
	}

	// ↓ to the second line of what you use, ↓ again over the heading to
	// the first smiley, → to the next.
	d.press(down, down, right)
	if got := d.m.emo.pick.shown().cells[d.m.emo.pick.at]; got != "smiley" || !d.has(":smiley:") {
		t.Fatalf("↓↓→ is on %q:\n%s", got, d.text)
	}
	d.press(enter)
	d.until("smiley sent", func() bool { return len(d.reacted()) == 1 && d.reacted()[0] == "smiley" })

	// It's first among what you use now; a click on the next, +1.
	d.press(r('r'))
	if got := d.m.emo.pick.shown().cells[0]; got != "smiley" {
		t.Fatalf("first is %q", got)
	}
	d.update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if d.m.emo.pick.top != 3 {
		t.Fatalf("the wheel scrolled to %d", d.m.emo.pick.top)
	}
	d.update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	d.clickCell(1, 1) // under the heading
	d.until("+1 sent", func() bool { return len(d.reacted()) == 2 && d.reacted()[1] == "+1" })
	if d.m.emo.pick.on {
		t.Fatal("a click should close the picker")
	}

	// The workspace's own, found by name and drawn as its picture.
	d.press(r('r'))
	d.typed("loaf")
	d.until("loaf's picture", func() bool { return hasPicture(d.m.render()) && d.has(":loaf:") })
	d.clickCell(0, 0) // no heading over what's found
	d.until("loaf sent", func() bool { return len(d.reacted()) == 3 && d.reacted()[2] == "loaf" })

	// A click outside closes it, reacting with nothing.
	d.press(r('r'))
	d.update(tea.MouseClickMsg{X: 1, Y: d.m.h - 2, Button: tea.MouseLeft})
	if d.m.emo.pick.on || len(d.reacted()) != 3 {
		t.Fatalf("a click outside should just close it:\n%s", d.text)
	}
}
