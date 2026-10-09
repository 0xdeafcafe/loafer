package store

import (
	"testing"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/jsonx"
)

func ev(t *testing.T, raw string) slack.Event {
	t.Helper()
	var e slack.Event
	if err := jsonx.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatal(err)
	}
	e.Raw = []byte(raw)
	return e
}

func TestApply(t *testing.T) {
	s := New()
	s.self = "UME"
	s.convs["C1"] = &Conv{ID: "C1", Kind: Channel, LastRead: "1.0", Latest: "1.0"}
	s.SetWindow("C1", []slack.Message{{TS: "1.0", User: "UA", Text: "hi"}}, false, false)

	msgs := func() []slack.Message { return s.windows["C1"].Msgs }
	s.Apply(ev(t, `{"type":"message","channel":"C1","ts":"2.0","user":"UB","text":"hey <@UME>"}`))
	s.Apply(ev(t, `{"type":"message","channel":"C1","ts":"2.0","user":"UB","text":"hey <@UME>"}`)) // twice
	if n := len(msgs()); n != 2 {
		t.Fatalf("a message told twice should be held once: %d", n)
	}
	if c := s.convs["C1"]; !c.Unread || c.Mentions != 2 || c.Latest != "2.0" {
		// Slack counts each mention event; the dedupe is of messages held.
		t.Fatalf("unread state: %+v", *c)
	}

	s.Apply(ev(t, `{"type":"message","channel":"C1","ts":"3.0","thread_ts":"2.0","user":"UA","text":"reply"}`))
	s.Apply(ev(t, `{"type":"message","channel":"C1","ts":"3.0","thread_ts":"2.0","user":"UA","text":"reply"}`))
	if p := msgs()[1]; len(msgs()) != 2 || p.ReplyCount != 1 || p.LatestReply != "3.0" || p.ReplyUsers[0] != "UA" {
		t.Fatalf("a reply should count once on its parent: %+v", p)
	}

	s.Apply(ev(t, `{"type":"message","subtype":"message_changed","channel":"C1","message":{"ts":"1.0","user":"UA","text":"hi again","edited":{}}}`))
	if m := msgs()[0]; m.Text != "hi again" || m.Edited == nil {
		t.Fatalf("edit: %+v", m)
	}

	for _, typ := range []string{"reaction_added", "reaction_added", "reaction_removed"} {
		s.Apply(ev(t, `{"type":"`+typ+`","user":"UB","reaction":"wave","item":{"channel":"C1","ts":"1.0"}}`))
	}
	if r := msgs()[0].Reactions; len(r) != 0 {
		t.Fatalf("added twice and removed once should leave none: %+v", r)
	}

	s.Apply(ev(t, `{"type":"channel_marked","channel":"C1","ts":"2.0"}`))
	if c := s.convs["C1"]; c.Unread || c.Mentions != 0 || c.LastRead != "2.0" {
		t.Fatalf("marked: %+v", *c)
	}

	s.Apply(ev(t, `{"type":"message","subtype":"message_deleted","channel":"C1","deleted_ts":"1.0"}`))
	if len(msgs()) != 1 || msgs()[0].TS != "2.0" {
		t.Fatalf("delete: %+v", msgs())
	}

	s.Apply(ev(t, `{"type":"message","channel":"D9","ts":"4.0","user":"UC","text":"long time"}`))
	if c := s.convs["D9"]; c == nil || c.Kind != IM || c.User != "UC" || c.Mentions != 1 {
		t.Fatalf("a DM not open should appear: %+v", c)
	}

	s.Apply(ev(t, `{"type":"channel_rename","channel":{"id":"C1","name":"renamed"}}`))
	s.Apply(ev(t, `{"type":"channel_left","channel":"D9"}`))
	if s.convs["C1"].Name != "renamed" || s.convs["D9"] != nil {
		t.Fatal("rename and leave")
	}
}
