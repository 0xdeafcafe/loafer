package ui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slacktest"
)

func (d *e2e) called(method string) (found bool) {
	for _, c := range d.srv.Calls() {
		found = found || c.Method == method
	}
	return found
}

func TestE2EPresence(t *testing.T) {
	d := newE2E(t)
	d.until("presence asked for", func() bool {
		subs := d.srv.Subscribed()
		return slices.Contains(subs, slacktest.Priya) && slices.Contains(subs, slacktest.Jo)
	})
	d.until("the dots", func() bool { return d.has("● priya") && d.has("○ jo") })

	d.srv.Presence(slacktest.Jo, "active")
	d.srv.Presence(slacktest.Priya, "away")
	d.until("their turn", func() bool { return d.has("● jo") && d.has("○ priya") })
}

func TestE2EStatus(t *testing.T) {
	d := newE2E(t)
	d.run(d.m.visit(slacktest.PriyaDM))
	d.until("priya's DM with her status", func() bool { return d.has("priya 🌴 on holiday") && d.has("got them, thanks") })
	if !d.has("priya 🌴 15") && !d.has("priya 🌴 ") {
		t.Fatalf("her messages have it too:\n%s", d.text)
	}

	d.srv.Push(`{"type":"user_status_changed","user":{"id":"` + slacktest.Priya + `","profile":{"status_emoji":"","status_text":""}}}`)
	d.until("her status cleared", func() bool { return !d.has("🌴") && d.has("got them, thanks") })
	d.srv.Push(`{"type":"user_change","user":{"id":"` + slacktest.Priya + `","name":"priya","real_name":"Priya Shah","profile":{"display_name":"priya","status_emoji":":tada:"}}}`)
	d.until("a new one", func() bool { return d.has("priya 🎉") })
}

func TestE2EProfileCard(t *testing.T) {
	d := newE2E(t)
	d.jump("general", slacktest.General)
	d.until("#general", func() bool { return d.has("projector cable") })

	// The newest message is tomás's, and he has no DM yet.
	d.press(tab, tab, r('p'))
	d.until("his card", func() bool { return d.has("Profile") && d.has("Tomás Reyes") && d.called("users.info") })
	d.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	d.until("a DM with him", func() bool {
		return d.m.open == "D0"+slacktest.Tomas && d.called("conversations.open") && d.has("● tomás   active")
	})
	if d.m.focus != onCompose {
		t.Fatalf("focus %v, want the box", d.m.focus)
	}

	// Priya's has more, and the DM already exists, so Slack isn't asked.
	d.run(d.m.visit(slacktest.PriyaDM))
	d.until("priya's DM", func() bool { return d.m.open == slacktest.PriyaDM })
	d.press(esc, r('p'))
	d.until("her card", func() bool {
		return d.has("Priya Shah") && d.has("Staff engineer") && d.has("@priya · she/her") && d.has("priya@crumb.example") && d.has("there · Asia/Kolkata")
	})
	n := len(d.srv.Calls())
	d.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.m.ppl.card.on {
		t.Fatal("enter should close the card")
	}
	for _, c := range d.srv.Calls()[n:] {
		if c.Method == "conversations.open" {
			t.Fatal("her DM is open already")
		}
	}
}

func TestE2EUserGroups(t *testing.T) {
	d := newE2E(t)
	d.jump("general", slacktest.General)
	d.until("#general", func() bool { return d.has("projector cable") })
	d.srv.Post(slacktest.General, slacktest.Jo, "<!subteam^"+slacktest.Bakers+"> lunch is at 12:30", "")
	d.until("the group by handle", func() bool { return d.has("@bakers lunch is at 12:30") })

	d.srv.Push(`{"type":"subteam_updated","subteam":{"id":"` + slacktest.Bakers + `","handle":"loaves","name":"Bakers"}}`)
	d.until("its new handle", func() bool { return d.has("@loaves lunch is at 12:30") })

	d.typed("hi @loa")
	d.until("the popup", func() bool { return d.has("@loaves") && d.has("Bakers") && d.m.pop.on })
}
