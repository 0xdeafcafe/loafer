package store

import (
	"fmt"
	"strings"
	"testing"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

func TestThreadLive(t *testing.T) {
	s := New()
	s.self = "UME"
	s.convs["C1"] = &Conv{ID: "C1", Kind: Channel}
	s.SetWindow("C1", []slack.Message{{TS: "1.0", User: "UA", Text: "parent", ReplyCount: 1, LatestReply: "1.5"}}, false, false)
	s.update(func() {
		s.putThread(threadKey("C1", "1.0"), []slack.Message{{TS: "1.0", User: "UA", Text: "parent", ReplyCount: 1}, {TS: "1.5", ThreadTS: "1.0", User: "UB", Text: "first"}})
	})
	thread := func() []slack.Message {
		var out []slack.Message
		s.Read(func(v View) { out = v.Thread("C1", "1.0").Msgs })
		return out
	}
	texts := func() string {
		var out []string
		for _, m := range thread() {
			out = append(out, m.Text)
		}
		return strings.Join(out, ",")
	}

	// A reply lands in the thread once, however often it's told, and the
	// parent there counts it; the conversation's parent counts it too.
	for range 2 {
		s.Apply(ev(t, `{"type":"message","channel":"C1","ts":"2.0","thread_ts":"1.0","user":"UA","text":"second"}`))
	}
	if texts() != "parent,first,second" || thread()[0].ReplyCount != 2 || s.windows["C1"].Msgs[0].ReplyCount != 2 {
		t.Fatalf("reply: %s, %+v", texts(), thread()[0])
	}
	if len(s.windows["C1"].Msgs) != 1 {
		t.Fatal("a reply isn't the conversation's")
	}
	// Sent to the channel too, it's both.
	s.Apply(ev(t, `{"type":"message","subtype":"thread_broadcast","channel":"C1","ts":"3.0","thread_ts":"1.0","user":"UA","text":"loud"}`))
	if texts() != "parent,first,second,loud" || len(s.windows["C1"].Msgs) != 2 {
		t.Fatalf("broadcast: %s", texts())
	}

	s.Apply(ev(t, `{"type":"message","subtype":"message_changed","channel":"C1","message":{"ts":"1.5","thread_ts":"1.0","user":"UB","text":"first, edited","edited":{}}}`))
	s.Apply(ev(t, `{"type":"reaction_added","user":"UB","reaction":"wave","item":{"channel":"C1","ts":"2.0"}}`))
	s.Apply(ev(t, `{"type":"message","subtype":"message_deleted","channel":"C1","deleted_ts":"3.0"}`))
	if texts() != "parent,first, edited,second" || thread()[1].Edited == nil || len(thread()[2].Reactions) != 1 {
		t.Fatalf("changed, reacted, deleted: %s %+v", texts(), thread())
	}

	// The channel's parent and the thread's don't share their reactions.
	s.Apply(ev(t, `{"type":"reaction_added","user":"UB","reaction":"eyes","item":{"channel":"C1","ts":"1.0"}}`))
	if n, m := s.windows["C1"].Msgs[0].Reactions, thread()[0].Reactions; len(n) != 1 || len(m) != 1 || n[0].Count != 1 || m[0].Count != 1 {
		t.Fatalf("parent reactions: %+v %+v", n, m)
	}
}

func TestThreadsEvict(t *testing.T) {
	s := New()
	for i := range keepThreads + 3 {
		s.update(func() {
			k := threadKey("C1", fmt.Sprint(i))
			s.putThread(k, nil)
			s.touchThread(k)
		})
	}
	s.Read(func(v View) {
		if len(s.threads) != keepThreads || v.Thread("C1", "0") != nil || v.Thread("C1", fmt.Sprint(keepThreads+2)) == nil {
			t.Fatalf("held %d threads", len(s.threads))
		}
	})
}
