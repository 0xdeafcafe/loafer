package store

import (
	"testing"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

// Your own reaction lands at once, and Slack's event for it finds it done.
func TestReact(t *testing.T) {
	s := New()
	s.self = "UME"
	s.SetWindow("C1", []slack.Message{{TS: "1.0", User: "UA"}}, false, false)
	got := func() []slack.Reaction { return s.windows["C1"].Msgs[0].Reactions }

	s.React("C1", "1.0", "tada", true)
	s.Apply(ev(t, `{"type":"reaction_added","user":"UME","reaction":"tada","item":{"channel":"C1","ts":"1.0"}}`))
	if r := got(); len(r) != 1 || r[0].Count != 1 || r[0].Users[0] != "UME" {
		t.Fatalf("added once: %+v", r)
	}
	s.React("C1", "1.0", "tada", false)
	s.React("C1", "1.0", "tada", false)
	s.React("C2", "1.0", "tada", true) // not held: nothing to do
	if r := got(); len(r) != 0 {
		t.Fatalf("removed: %+v", r)
	}

	s.ApplyEmoji(map[string]string{"party": "https://x/p.png", "ab": "alias:party"})
	s.Read(func(v View) {
		if n := v.CustomEmoji(); len(n) != 2 || n[0] != "ab" {
			t.Fatalf("custom: %v", n)
		}
	})
}
