package store

import (
	"testing"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

func TestActions(t *testing.T) {
	s := New()
	s.ApplyBoot(slack.UserBoot{Channels: []slack.Conversation{{ID: "C1", Name: "dev", IsChannel: true, IsMember: true, LastRead: "5.0"}}})
	s.SetWindow("C1", []slack.Message{{TS: "3.0"}, {TS: "2.0"}, {TS: "1.0"}}, false, false)

	s.MarkUnread("C1", "1.0")
	s.Read(func(v View) {
		if c := v.Conv("C1"); !c.Unread || c.LastRead != "1.0" {
			t.Errorf("mark unread: %+v", c)
		}
	})

	// A pin lands on the window's message, whatever else holds a copy, and
	// goes again.
	s.SetPinned("C1", "2.0", true)
	s.SetPinned("C1", "2.0", true)
	s.Read(func(v View) {
		if p := v.Window("C1").Msgs[1].PinnedTo; len(p) != 1 || p[0] != "C1" {
			t.Errorf("pinned to %v", p)
		}
	})
	s.SetPinned("C1", "2.0", false)
	s.Read(func(v View) {
		if p := v.Window("C1").Msgs[1].PinnedTo; len(p) != 0 {
			t.Errorf("still pinned to %v", p)
		}
	})

	// The websocket's word is the same.
	s.Apply(slack.Event{Type: "pin_added", Raw: []byte(`{"type":"pin_added","channel_id":"C1","item":{"type":"message","message":{"ts":"3.0"}}}`)})
	s.Read(func(v View) {
		if len(v.Window("C1").Msgs[2].PinnedTo) != 1 {
			t.Error("pin_added not applied")
		}
	})

	// Later: nothing to keep in step until it's been fetched.
	s.SaveLater("C1", slack.Message{TS: "2.0"}, 0)
	s.Read(func(v View) {
		if v.IsSaved("C1", "2.0") {
			t.Error("saved into a list that isn't held")
		}
	})
	s.update(func() { s.tabs.loaded[LaterList] = true })
	s.SaveLater("C1", slack.Message{TS: "2.0"}, 0)
	s.SaveLater("C1", slack.Message{TS: "2.0"}, 99)
	s.Read(func(v View) {
		if got := v.Saved(); !v.IsSaved("C1", "2.0") || len(got) != 1 || got[0].Due != 99 {
			t.Errorf("saved %+v", got)
		}
	})
	s.Unsave("C1", "2.0")
	s.Read(func(v View) {
		if v.IsSaved("C1", "2.0") {
			t.Error("still saved")
		}
	})
}
