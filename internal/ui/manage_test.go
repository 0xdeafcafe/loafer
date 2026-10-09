package ui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slacktest"
	"github.com/0xdeafcafe/loafer/internal/store"
)

// selectSide puts the sidebar cursor on conv (or the section heading #id).
func (d *e2e) selectSide(key string) {
	d.t.Helper()
	i := slices.IndexFunc(d.m.side, func(it sideItem) bool { return it.key() == key })
	if i < 0 {
		d.t.Fatalf("%q isn't in the sidebar:\n%s", key, d.text)
	}
	d.m.sideAt = i
	d.m.focus = onSide
	d.text = ""
	d.update(tea.WindowSizeMsg{Width: 120, Height: 60})
}

func (d *e2e) called(method, key, val string) bool {
	for _, c := range d.srv.Calls() {
		if c.Method == method && (key == "" || c.Form.Get(key) == val) {
			return true
		}
	}
	return false
}

func (d *e2e) conv(id string) (c store.Conv, ok bool) {
	d.m.st.Read(func(v store.View) {
		if x := v.Conv(id); x != nil {
			c, ok = *x, true
		}
	})
	return c, ok
}

func TestE2EBrowseAndJoin(t *testing.T) {
	d := newE2E(t)
	d.press(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	d.typed("#")
	d.until("the browser", func() bool { return d.has("Browse channels") && d.has("bookclub") })
	if d.has("old-launch") || len(d.m.mg.pk.items) != 1 {
		t.Fatalf("the browser should hold only bookclub (not archived, not joined):\n%s", d.text)
	}
	if !d.has("one chapter a fortnight") || !d.has("7 ⊙") {
		t.Fatalf("purpose and member count missing:\n%s", d.text)
	}

	// enter reads it, with a join chip where the box would be.
	d.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	d.until("the preview", func() bool { return d.m.open == slacktest.Books && d.has("chapter four is the one with the bakery") })
	if !d.has("reading # bookclub") || !d.has(" join ") || d.has("enter sends") {
		t.Fatalf("preview should carry a join chip and no box:\n%s", d.text)
	}
	if c, _ := d.conv(slacktest.Books); !c.Preview {
		t.Fatal("not a preview")
	}
	if slices.ContainsFunc(d.m.side, func(it sideItem) bool { return it.conv == slacktest.Books }) {
		t.Fatal("the preview is in the sidebar")
	}
	d.press(r('x')) // keys in the messages don't write, and nothing leaves
	if len(d.m.input) != 0 {
		t.Fatal("typed into a preview")
	}

	// tab comes to the chip; j joins.
	d.press(tab)
	if d.m.focus != onCompose {
		t.Fatalf("focus %v", d.m.focus)
	}
	d.press(r('j'))
	d.until("joined", func() bool { c, _ := d.conv(slacktest.Books); return !c.Preview && d.has("to # bookclub") })
	if d.called("conversations.mark", "channel", slacktest.Books) {
		t.Fatal("marked a channel read that wasn't joined")
	}
	if !d.called("conversations.join", "channel", slacktest.Books) {
		t.Fatal("never asked Slack to join")
	}
	if !slices.ContainsFunc(d.m.side, func(it sideItem) bool { return it.conv == slacktest.Books }) {
		t.Fatalf("not in the sidebar:\n%s", d.text)
	}
}

func TestE2ENewDM(t *testing.T) {
	d := newE2E(t)

	// Someone with a DM already: no call, just there.
	d.press(r('N'))
	d.until("the picker", func() bool { return d.has("New message") })
	d.typed("priya")
	d.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.m.open != slacktest.PriyaDM || d.called("conversations.open", "", "") {
		t.Fatalf("open %q", d.m.open)
	}

	// Someone without.
	d.m.focus = onSide
	d.press(r('N'))
	d.typed("tom")
	d.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	d.until("a DM with tomás", func() bool {
		return d.called("conversations.open", "users", slacktest.Tomas) && d.m.open != slacktest.PriyaDM
	})
	if c, ok := d.conv(d.m.open); !ok || c.Kind != store.IM || c.User != slacktest.Tomas {
		t.Fatalf("opened %+v", c)
	}
	d.until("in the sidebar", func() bool { return d.has("● tomás") && d.m.focus == onCompose })

	// Several: tab picks each, enter opens the group.
	d.m.focus = onSide
	d.press(r('N'))
	d.typed("tom")
	d.press(tab)
	d.typed("jo")
	d.press(tab)
	d.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	d.until("a group DM", func() bool { return d.called("conversations.open", "users", slacktest.Tomas+","+slacktest.Jo) })
	d.until("it opens", func() bool {
		c, ok := d.conv(d.m.open)
		return ok && c.Kind == store.MPIM && d.has("⁂ tomás, jo")
	})
}

func TestE2ELeaveAndClose(t *testing.T) {
	d := newE2E(t)

	// Leaving asks first, and any key but y says no.
	d.selectSide(slacktest.Random)
	d.press(r('x'))
	if !d.has("leave # random?") {
		t.Fatalf("no question:\n%s", d.text)
	}
	d.press(r('n'))
	if _, ok := d.conv(slacktest.Random); !ok || d.called("conversations.leave", "", "") {
		t.Fatal("left without a yes")
	}
	d.press(r('x'), r('y'))
	d.until("left", func() bool { _, ok := d.conv(slacktest.Random); return !ok && d.has("left # random") })
	d.until("told Slack", func() bool { return d.called("conversations.leave", "channel", slacktest.Random) })

	// A DM just closes.
	d.selectSide(slacktest.JoDM)
	d.press(r('x'))
	d.until("closed", func() bool { _, ok := d.conv(slacktest.JoDM); return !ok })
	d.until("told Slack", func() bool { return d.called("conversations.close", "channel", slacktest.JoDM) })
	if d.m.open == slacktest.JoDM || d.m.open == slacktest.Random {
		t.Fatalf("still open: %q", d.m.open)
	}
}

func TestE2EFold(t *testing.T) {
	d := newE2E(t)
	if !d.has("⊡ design") {
		t.Fatalf("design should show:\n%s", d.text)
	}
	d.selectSide("#S0TEAM")
	d.press(r('z'))
	// Folded: design is quiet and goes, general has a mention and stays.
	if d.has("⊡ design") || !d.has("# general") || !d.has("▸ ") {
		t.Fatalf("folded section:\n%s", d.text)
	}
	if d.m.side[d.m.sideAt].id != "S0TEAM" {
		t.Fatal("the cursor should rest on the heading")
	}
	d.until("saved", func() bool { x, _ := d.srv.Section("S0TEAM"); return x.Collapsed })

	// Folded conversations are still there to jump to.
	d.jump("design", slacktest.Design)

	// enter on the heading opens it again.
	d.selectSide("#S0TEAM")
	d.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	d.until("unfolded", func() bool { x, _ := d.srv.Section("S0TEAM"); return !x.Collapsed && d.has("⊡ design") })
}

func TestE2EMute(t *testing.T) {
	d := newE2E(t)
	d.selectSide(slacktest.Alerts)
	d.press(r('m'))
	d.until("muted", func() bool { return d.srv.Pref("muted_channels") == slacktest.Alerts })
	if c, _ := d.conv(slacktest.Alerts); !c.Muted {
		t.Fatal("not muted here")
	}

	// A muted conversation going unread isn't counted or jumped to.
	d.srv.Post(slacktest.Alerts, slacktest.Tomas, "is staging down for anyone else", "")
	d.until("#alerts unread", func() bool { return d.unread(slacktest.Alerts) })
	if !d.has("3 unread") || d.has("4 unread") || needsYou(&store.Conv{Muted: true, Mentions: 1}) || unreadConv(&store.Conv{Muted: true, Unread: true}) {
		t.Fatalf("muted should not count:\n%s", d.text)
	}

	d.selectSide(slacktest.Alerts)
	d.press(r('m'))
	d.until("unmuted", func() bool { return d.srv.Pref("muted_channels") == "" && d.has("4 unread") })
}

func TestE2EMoveAndStar(t *testing.T) {
	d := newE2E(t)
	in := func(section, conv string) bool {
		x, _ := d.srv.Section(section)
		return slices.Contains(x.Channels.IDs, conv)
	}

	// s opens the chooser on where it is now; k goes up one to Team.
	d.selectSide(slacktest.Random)
	d.press(r('s'))
	if !d.has("Move # random to") || !d.has("Starred") || !d.has("Team") {
		t.Fatalf("no chooser:\n%s", d.text)
	}
	d.press(r('k'), tea.KeyPressMsg{Code: tea.KeyEnter})
	d.until("moved", func() bool { return in("S0TEAM", slacktest.Random) })
	var team []string
	d.m.st.Read(func(v store.View) {
		for _, s := range v.Sidebar() {
			if s.ID == "S0TEAM" {
				team = s.Convs
			}
		}
	})
	if !slices.Contains(team, slacktest.Random) {
		t.Fatalf("not in Team here: %v", team)
	}

	// * stars it, and again takes it back to its kind's section.
	d.selectSide(slacktest.Random)
	d.press(r('*'))
	d.until("starred", func() bool { return in("S0STARS", slacktest.Random) && !in("S0TEAM", slacktest.Random) })
	d.selectSide(slacktest.Random)
	d.press(r('*'))
	d.until("unstarred", func() bool { return !in("S0STARS", slacktest.Random) })
	d.m.st.Read(func(v store.View) {
		if v.SectionOf(slacktest.Random) != "" {
			t.Fatal("should be back with its kind")
		}
	})
}

// A section deleted elsewhere arrives over the socket, and starring then
// has nowhere to go.
func TestE2EStarWithoutSection(t *testing.T) {
	d := newE2E(t)
	d.srv.Push(`{"type":"channel_section_deleted","channel_section_id":"S0STARS"}`)
	d.until("the section gone", func() bool {
		var n int
		d.m.st.Read(func(v store.View) { n = len(v.Sections()) })
		return n == 4
	})
	d.selectSide(slacktest.General)
	d.press(r('*'))
	if !d.has("no Starred section") {
		t.Fatalf("starring with no Starred section:\n%s", d.text)
	}
}
