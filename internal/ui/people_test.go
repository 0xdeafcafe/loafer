package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/loafer/internal/mrkdwn"
	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/jsonx"
)

func frameText(m *Model) string { return strings.Join(plainFrame(m.render()), "\n") }

// slackEvent is a websocket frame as the store takes it.
func slackEvent(t *testing.T, raw string) slack.Event {
	t.Helper()
	var e slack.Event
	if err := jsonx.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatal(err)
	}
	e.Raw = []byte(raw)
	return e
}

func userOf(t *testing.T, raw string) slack.User {
	t.Helper()
	var u slack.User
	if err := jsonx.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestDMPresence(t *testing.T) {
	m := fixture(t)
	if text := frameText(m); !strings.Contains(text, "● drew") {
		t.Fatalf("a DM nobody has said anything about is a plain dot:\n%s", text)
	}
	m.st.Apply(slackEvent(t, `{"type":"presence_change","user":"U1","presence":"away"}`))
	if text := frameText(m); !strings.Contains(text, "○ drew") {
		t.Fatalf("away is hollow:\n%s", text)
	}
	m.st.Apply(slackEvent(t, `{"type":"presence_change","users":["U1"],"presence":"active"}`))
	if text := frameText(m); !strings.Contains(text, "● drew") {
		t.Fatalf("active is solid:\n%s", text)
	}

	// The DM's header has it, and its status.
	m.openConv("D1")
	m.st.ApplyUser(userOf(t, `{"id":"U1","name":"drew","profile":{"status_emoji":":tada:","status_text":"launching"}}`))
	if text := frameText(m); !strings.Contains(text, "● drew 🎉 launching   active") {
		t.Fatalf("DM header:\n%s", text)
	}
}

func TestStatusBesideNames(t *testing.T) {
	m := fixture(t)
	if text := frameText(m); strings.Contains(text, "drew 🎉") {
		t.Fatal("no status yet")
	}
	m.st.ApplyUser(userOf(t, `{"id":"U1","name":"drew","profile":{"status_emoji":":tada:"}}`))
	if text := frameText(m); !strings.Contains(text, "drew 🎉") {
		t.Fatalf("a status shows in message headers, and the rows were drawn again:\n%s", text)
	}
	// One that has run out doesn't.
	m.st.ApplyUser(userOf(t, `{"id":"U1","name":"drew","profile":{"status_emoji":":tada:","status_expiration":1}}`))
	if text := frameText(m); strings.Contains(text, "drew 🎉") {
		t.Fatalf("an expired status is gone:\n%s", text)
	}
}

func TestProfileCard(t *testing.T) {
	m := fixture(t)
	ends := time.Now().Add(72 * time.Hour)
	m.st.ApplyUser(userOf(t, fmt.Sprintf(`{"id":"U1","name":"drew","real_name":"Drew Dunn","tz":"Asia/Kolkata","profile":{
		"title":"Chef","email":"drew@example.com","pronouns":"they/them",
		"status_emoji":":palm_tree:","status_text":"on holiday","status_expiration":%d}}`, ends.Unix())))
	m.st.Apply(slackEvent(t, `{"type":"presence_change","user":"U1","presence":"active"}`))

	press(m, tab)
	m.pick(oldest)
	press(m, r('p'))
	if !m.ppl.card.on || m.ppl.card.id != "U0" { // the oldest message is yours
		t.Fatalf("card for %+v", m.ppl.card)
	}
	press(m, esc)
	if m.ppl.card.on || m.focus != onMsgs {
		t.Fatalf("esc closes the card and nothing else: card %v focus %v", m.ppl.card.on, m.focus)
	}
	m.pick(by(1))
	press(m, r('p'))
	text := frameText(m)
	for _, want := range []string{"Profile", "Drew Dunn", "@drew · they/them", "Chef", "● active", "🌴 on holiday · until " + strings.ToLower(ends.Format("2 Jan")), "there · Asia/Kolkata", "drew@example.com", "enter message", "y copy @drew"} {
		if !m.ppl.card.on || !strings.Contains(text, want) {
			t.Fatalf("card lacks %q:\n%s", want, text)
		}
	}

	// Its keys are its own: j doesn't move the cursor behind it.
	sel := m.sel
	press(m, r('j'))
	if !m.ppl.card.on || m.sel != sel {
		t.Fatal("j should not move the cursor behind the card")
	}
	_, cmd := m.Update(r('y'))
	if m.ppl.card.on || cmd == nil || !strings.Contains(m.flash, "copied @drew") {
		t.Fatalf("y: card %v, flash %q", m.ppl.card.on, m.flash)
	}

	// In a DM, p needs no message picked.
	m.openConv("D1")
	m.setFocus(onMsgs)
	m.sel = ""
	press(m, r('p'))
	if !m.ppl.card.on || m.ppl.card.id != "U1" {
		t.Fatalf("DM card: %+v", m.ppl.card)
	}
	// A terminal too narrow for a card draws without it.
	m.w = 30
	m.render()
}

func TestUserGroups(t *testing.T) {
	m := composeFixture(t)
	m.st.ApplyGroups([]slack.Group{{ID: "S1", Handle: "bakers", Name: "The Bakers"}, {ID: "S2", Handle: "gone", Deleted: 1}})
	typeText(m, "hi @bak")
	if len(m.pop.items) == 0 || m.pop.items[0].label != "@bakers" || m.pop.items[0].detail != "The Bakers" {
		t.Fatalf("popup: %v", popLabels(m))
	}
	press(m, cEnter)
	if got := encode(m.input, m.ments); got != "hi <!subteam^S1|@bakers> " {
		t.Fatalf("encode %q", got)
	}
	if string(m.input) != "hi @bakers " {
		t.Fatalf("the box keeps it readable: %q", string(m.input))
	}

	// What's in a message reads as the handle, whichever way it came; and
	// editing one turns it back into a piece.
	m.st.Read(func(v store.View) {
		for _, text := range []string{"<!subteam^S1>", "<!subteam^S1|@old>"} {
			if got := plainText(v, text); got != "@bakers" {
				t.Errorf("%s reads %q", text, got)
			}
		}
		if got := plainText(v, "<!subteam^S9>"); got != "@S9" {
			t.Errorf("an unknown group reads %q", got)
		}
		in, ments := decode(v, "ping <!subteam^S1> now")
		if string(in) != "ping @bakers now" || len(ments) != 1 || ments[0].code != "<!subteam^S1>" {
			t.Errorf("decode %q %v", string(in), ments)
		}
		sp := mrkdwn.Span{Kind: mrkdwn.Group, Target: "S1", Text: "@S1"} // rich text brings the id for both
		if got := spanText(v, sp); got != "@bakers" {
			t.Errorf("rich text group reads %q", got)
		}
	})
}
