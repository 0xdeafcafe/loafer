package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/slacktest"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/theme"
)

// multiE2E is e2e for a Multi over two fake workspaces at once: Crumb &
// Co shown, Rye Labs behind it.
type multiE2E struct {
	t    *testing.T
	x    *Multi
	a, b *slacktest.Server
	ctx  context.Context
	msgs chan tea.Msg
	text string
}

func newMultiE2E(t *testing.T, before func(b *slacktest.Server)) *multiE2E {
	a, b := slacktest.New(), slacktest.NewTeam("T0RYE", "Rye Labs", "#0b4f6c")
	if before != nil {
		before(b)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); a.Close(); b.Close() })
	d := &multiE2E{t: t, a: a, b: b, ctx: ctx, msgs: make(chan tea.Msg, 64)}
	d.x = NewMulti(New(ctx, store.New(), a.Client()), New(ctx, store.New(), b.Client()))
	d.update(tea.WindowSizeMsg{Width: 124, Height: 60})
	d.run(d.x.Init())
	return d
}

func (d *multiE2E) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				d.run(c)
			}
			return
		}
		if msg != nil {
			select {
			case d.msgs <- msg:
			case <-d.ctx.Done():
			}
		}
	}()
}

func (d *multiE2E) shown() *Model { return d.x.ws[d.x.at] }

func (d *multiE2E) update(msg tea.Msg) {
	_, cmd := d.x.Update(msg)
	d.run(cmd)
	m := d.shown()
	d.text = strings.Join(plainFrame(m.railed(m.render())), "\n")
}

func (d *multiE2E) press(keys ...tea.KeyPressMsg) {
	for _, k := range keys {
		d.update(k)
	}
}

func (d *multiE2E) has(s string) bool { return strings.Contains(d.text, s) }

func (d *multiE2E) until(what string, ok func() bool) {
	d.t.Helper()
	deadline := time.After(3 * time.Second)
	for !ok() {
		select {
		case msg := <-d.msgs:
			d.update(msg)
		case <-deadline:
			d.t.Fatalf("waited for %s:\n%s", what, d.text)
		}
	}
}

func called(s *slacktest.Server, method, channel string) bool {
	for _, c := range s.Calls() {
		if c.Method == method && (channel == "" || c.Form.Get("channel") == channel) {
			return true
		}
	}
	return false
}

func TestE2EWorkspaces(t *testing.T) {
	d := newMultiE2E(t, nil)
	front, back := d.x.ws[0], d.x.ws[1]
	d.until("both live", func() bool {
		return front.open != "" && front.live == "live" && back.live == "live" && d.has("● live")
	})
	// The rail has both, Crumb's marked, each with boot's two mentions.
	if !d.has("▍CC") || !d.has(" RL") || strings.Count(d.text, " @2 ") != 2 {
		t.Fatalf("rail:\n%s", d.text)
	}
	// Rye Labs, behind, opened nothing and fetched no messages.
	if back.open != "" || called(d.b, "conversations.history", "") {
		t.Fatalf("the workspace behind fetched messages: open %q", back.open)
	}

	// Somewhere to be, with a draft.
	d.press(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	for _, c := range "general" {
		d.press(r(c))
	}
	d.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	if front.open != slacktest.General {
		t.Fatalf("jumped to %q", front.open)
	}
	for _, c := range "half-baked" {
		d.press(r(c))
	}

	// A message lands in Rye Labs, behind: unread there, and on the rail,
	// but not read, and not on the screen.
	d.b.Post(slacktest.Dev, slacktest.Priya, "rye rises slower", "")
	d.until("rye's #dev unread", func() bool {
		unread := false
		back.st.Read(func(v store.View) { unread = v.Conv(slacktest.Dev).Unread })
		return unread
	})
	if d.has("rye rises slower") || called(d.b, "conversations.mark", "") {
		t.Fatalf("the workspace behind showed or read it:\n%s", d.text)
	}

	// alt+w goes there: its header, its conversation opened now.
	d.press(tea.KeyPressMsg{Code: 'w', Mod: tea.ModAlt})
	if d.shown() != back {
		t.Fatal("alt+w didn't switch")
	}
	d.until("rye labs open", func() bool { return d.has("Rye Labs") && back.open != "" && d.has(" CC") && d.has("▍RL") })

	// ctrl+k lists the other workspace; enter goes back, to where it was.
	d.press(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	for _, c := range "crumb" {
		d.press(r(c))
	}
	if !d.has("Crumb & Co") {
		t.Fatalf("ctrl+k lacks the workspace:\n%s", d.text)
	}
	d.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	d.until("back in crumb", func() bool { return d.shown() == front })
	if front.open != slacktest.General || string(front.input) != "half-baked" {
		t.Fatalf("crumb lost its place: open %q, draft %q", front.open, string(front.input))
	}
}

func TestE2EWorkspaceSignedOut(t *testing.T) {
	d := newMultiE2E(t, func(b *slacktest.Server) { b.SetSignedOut(true) })
	front, back := d.x.ws[0], d.x.ws[1]
	d.until("crumb live and rye signed out", func() bool {
		return front.live == "live" && back.SignedOut() && d.has("✗")
	})
	if !d.has("signed you out of Rye Labs · run loafer login") {
		t.Fatalf("no word of it:\n%s", d.text)
	}
	if d.x.SignedOut() {
		t.Fatal("one signed out shouldn't be all")
	}
	// Crumb carries on. (#design, as what opened first depends on whether
	// the sections beat boot.)
	d.a.Post(slacktest.Design, slacktest.Tomas, "still here", "")
	d.until("crumb's #design unread", func() bool { return d.has("4 unread") })
}

func TestTeamInitials(t *testing.T) {
	for name, want := range map[string]string{"Crumb & Co": "CC", "LangWatch": "La", "x": "X ", "": "··", "the 2nd shift": "T2"} {
		if got := teamInitials(name); got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
}

func TestTeamColour(t *testing.T) {
	if got := teamColour(slack.Team{ID: "T1", Colour: "#0b4f6c"}); got != rgb(0x0b, 0x4f, 0x6c) {
		t.Errorf("theme colour: %v", got)
	}
	a, b := teamColour(slack.Team{ID: "T0CRUMB"}), teamColour(slack.Team{ID: "T0CRUMB", Colour: "nope"})
	if a != b {
		t.Errorf("the hash should be stable: %v %v", a, b)
	}
}

func TestWashReads(t *testing.T) {
	p := NewPalette(theme.Dark, Aubergine, false)
	for _, to := range append(wsColours, rgb(248, 248, 250), rgb(255, 230, 0)) {
		cols := washColours(p.Side, to, 120)
		if cols[0] != p.Side.Ground || cols[71] != p.Side.Ground {
			t.Fatal("the first 60% should be plain")
		}
		for _, c := range cols {
			if theme.Contrast(p.Side.Dim.FG, c) < min(3, theme.Contrast(p.Side.Dim.FG, p.Side.Ground))-0.01 {
				t.Fatalf("dim unreadable on %v washing to %v", c, to)
			}
		}
	}
}

func TestTitled(t *testing.T) {
	m := fixture(t)
	if got := m.titled("#dev"); got != "#dev" {
		t.Errorf("one workspace: %q", got)
	}
	NewMulti(m, fixture(t))
	if got := m.titled("#dev"); !strings.HasSuffix(got, " · #dev") || got == " · #dev" {
		t.Errorf("several: %q", got)
	}
}
