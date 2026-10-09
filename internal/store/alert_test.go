package store

import (
	"strconv"
	"testing"
	"time"

	"github.com/0xdeafcafe/loafer/internal/notify"
	"github.com/0xdeafcafe/loafer/internal/slack"
)

func note(s *Store) (n notify.Note, ok bool) {
	select {
	case n = <-s.Notes():
		return n, true
	default:
		return n, false
	}
}

func TestNotes(t *testing.T) {
	s := New()
	var b slack.UserBoot
	b.Self.ID = "UME"
	b.Channels = []slack.Conversation{{ID: "C1", Name: "dev", IsChannel: true, IsMember: true}, {ID: "C2", Name: "ops", IsChannel: true, IsMember: true}}
	b.IMs = []slack.Conversation{{ID: "D1", User: "UA", IsIM: true}}
	b.Prefs = []byte(`{"muted_channels":"C2","highlight_words":"deploy"}`)
	s.ApplyBoot(b)
	s.ApplyPeople([]slack.User{{ID: "UA", Name: "drew"}, {ID: "UME", Name: "alex"}})

	s.Apply(ev(t, `{"type":"message","channel":"C1","ts":"1.0","user":"UA","text":"anyone about?"}`))
	s.Apply(ev(t, `{"type":"message","channel":"C2","ts":"2.0","user":"UA","text":"<@UME> hi"}`))
	s.Apply(ev(t, `{"type":"message","channel":"C1","ts":"3.0","user":"UME","text":"<@UME> me"}`))
	if n, ok := note(s); ok {
		t.Fatalf("nothing here should notify: %+v", n)
	}

	s.Apply(ev(t, `{"type":"message","channel":"C1","ts":"4.0","user":"UA","text":"hey <@UME>, see *this*"}`))
	if n, ok := note(s); !ok || n.Title != "#dev" || n.Body != "drew: hey @alex, see this" {
		t.Fatalf("a mention: %+v", n)
	}
	s.Apply(ev(t, `{"type":"message","channel":"D1","ts":"5.0","user":"UA","text":"psst"}`))
	if n, ok := note(s); !ok || n.Title != "drew" || n.Body != "psst" {
		t.Fatalf("a dm: %+v", n)
	}

	s.Watch("D1", true)
	s.Apply(ev(t, `{"type":"message","channel":"D1","ts":"6.0","user":"UA","text":"still there?"}`))
	s.Watch("D1", false)
	if n, ok := note(s); ok {
		t.Fatalf("not for the conversation you're looking at: %+v", n)
	}
	s.Apply(ev(t, `{"type":"message","channel":"D1","ts":"7.0","user":"UA","text":"hello?"}`))
	if _, ok := note(s); !ok {
		t.Fatal("it should when the terminal is out of focus")
	}

	s.Apply(ev(t, `{"type":"pref_change","name":"muted_channels","value":"D1"}`))
	s.Apply(ev(t, `{"type":"message","channel":"D1","ts":"8.0","user":"UA","text":"hello??"}`))
	if _, ok := note(s); ok {
		t.Fatal("pref_change should mute it")
	}
	s.Apply(ev(t, `{"type":"pref_change","name":"muted_channels","value":""}`))
	until := time.Now().Add(time.Hour).Unix()
	s.Apply(ev(t, `{"type":"dnd_updated","user":"UME","dnd_status":{"snooze_enabled":true,"snooze_endtime":`+strconv.FormatInt(until, 10)+`}}`))
	s.Apply(ev(t, `{"type":"message","channel":"D1","ts":"9.0","user":"UA","text":"hello???"}`))
	if _, ok := note(s); ok {
		t.Fatal("a snooze should quiet it")
	}
}

func TestThreadFollowed(t *testing.T) {
	s := New()
	s.convs["C1"] = &Conv{ID: "C1", Name: "dev", Kind: Channel}
	s.al.prefs.Load(nil, nil)
	s.self = "UME"
	s.SetWindow("C1", []slack.Message{{TS: "2.0", User: "UA", Text: "theirs"}, {TS: "1.0", User: "UME", Text: "mine"}}, false, false)
	s.Apply(ev(t, `{"type":"message","channel":"C1","ts":"3.0","thread_ts":"2.0","user":"UB","text":"reply"}`))
	if _, ok := note(s); ok {
		t.Fatal("a reply in someone else's thread isn't yours")
	}
	s.Apply(ev(t, `{"type":"message","channel":"C1","ts":"4.0","thread_ts":"1.0","user":"UB","text":"reply"}`))
	if n, ok := note(s); !ok || n.Body != "UB: reply" || n.Conv != "C1" || n.TS != "4.0" || n.Thread != "1.0" {
		t.Fatalf("a reply to your thread: %+v", n)
	}
}

func TestTyping(t *testing.T) {
	s := New()
	s.self = "UME"
	s.Watch("C1", true)
	for _, u := range []string{"UB", "UA", "UME"} {
		s.Apply(ev(t, `{"type":"user_typing","channel":"C1","user":"`+u+`"}`))
	}
	s.Apply(ev(t, `{"type":"user_typing","channel":"C1","user":"UC","thread_ts":"1.0"}`)) // in a thread
	select {
	case <-s.Changed():
	default:
		t.Fatal("typing in the open conversation should wake the UI")
	}
	if s.Version() != 0 {
		t.Fatal("typing shouldn't count as a change to the store")
	}
	now := time.Now()
	s.Read(func(v View) {
		ids, until := v.Typing("C1", now)
		if len(ids) != 2 || ids[0] != "UA" || ids[1] != "UB" || !until.After(now) || until.After(now.Add(TypingFor)) {
			t.Fatalf("typing: %v until %v", ids, until)
		}
		if ids, _ := v.Typing("C1", now.Add(TypingFor+time.Second)); len(ids) != 0 {
			t.Fatalf("typing should lapse: %v", ids)
		}
	})
	s.Apply(ev(t, `{"type":"message","channel":"C1","ts":"1.0","user":"UA","text":"done"}`))
	s.Read(func(v View) {
		if ids, _ := v.Typing("C1", now); len(ids) != 1 || ids[0] != "UB" {
			t.Fatalf("sending should stop the typing: %v", ids)
		}
	})
	s.Watch("C2", true)
	<-s.Changed() // the message's
	s.Apply(ev(t, `{"type":"user_typing","channel":"C1","user":"UA"}`))
	select {
	case <-s.Changed():
		t.Fatal("typing elsewhere shouldn't wake the UI")
	default:
	}
}
