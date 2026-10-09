package ui

import (
	"strings"
	"testing"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/jsonx"
)

// Reactions as conversations.history sends them, skin tones and custom
// emoji too, draw as pills; narrow, they wrap a pill at a time; and a
// live reaction draws only its own message again.
func TestReactionPills(t *testing.T) {
	m := fixture(t)
	var msgs []slack.Message
	raw := `[{"ts":"1700000000.000100","user":"U1","text":"pills","reactions":[
		{"name":"+1::skin-tone-2","users":["U1"],"count":1},
		{"name":"partyparrot","users":["U1","U2","U3"],"count":3},
		{"name":"heart","users":["U0","U1"],"count":2}]}]`
	if err := jsonx.Unmarshal([]byte(raw), &msgs); err != nil {
		t.Fatal(err)
	}
	m.st.SetWindow("C1", msgs, false, false)
	text := strings.Join(plainFrame(m.render()), "\n")
	for _, want := range []string{" 👍🏻 1 ", " :partyparrot: 3 ", " ❤️ 2 "} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q pill:\n%s", want, text)
		}
	}

	var rows []canvas.Row
	m.st.Read(func(v store.View) { rows = reactionRows(&m.pal, v, msgs[0].Reactions, 20) })
	if len(rows) < 2 {
		t.Fatalf("20 wide should wrap: %d rows", len(rows))
	}
	for _, r := range rows {
		if r.Width() > 20 {
			t.Fatalf("a row %d wide", r.Width())
		}
	}

	n := m.hov.n
	m.st.Apply(slack.Event{Type: "reaction_added", Raw: []byte(`{"type":"reaction_added","user":"U2","reaction":"heart","item":{"channel":"C1","ts":"1700000000.000100"}}`)})
	if text := strings.Join(plainFrame(m.render()), "\n"); !strings.Contains(text, " ❤️ 3 ") {
		t.Fatalf("the live reaction didn't land:\n%s", text)
	}
	if d := m.hov.n - n; d != 1 {
		t.Fatalf("a reaction drew %d messages again, want 1", d)
	}
}
