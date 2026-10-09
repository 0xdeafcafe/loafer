package store

import (
	"encoding/json/jsontext"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

func sections(s *Store) {
	mk := func(id, name, typ, next string, ids ...string) slack.Section {
		x := slack.Section{ID: id, Name: name, Type: typ, Next: next}
		x.Channels.IDs = ids
		return x
	}
	s.ApplySections([]slack.Section{
		mk("s0", "Starred", "stars", "s1"),
		mk("s1", "Team", "standard", "s2", "C1"),
		mk("s2", "Channels", "channels", ""),
	})
}

func names(s *Store) (out []string) {
	s.Read(func(v View) {
		for _, sec := range v.Sidebar() {
			out = append(out, sec.Name+":"+strings.Join(sec.Convs, ","))
		}
	})
	return out
}

func TestMuteFollowsPrefs(t *testing.T) {
	s := New()
	s.ApplyBoot(boot())
	if v := s.Mute("C1", true); v != "C1" {
		t.Fatalf("value %q", v)
	}
	muted := func(id string) (m bool) { s.Read(func(v View) { m = v.Conv(id).Muted }); return m }
	if !muted("C1") || muted("C2") {
		t.Fatal("C1 should be muted and C2 not")
	}
	// Slack's own word wins: pref_change replaces the list.
	s.Apply(slack.Event{Type: "pref_change", Raw: jsontext.Value(`{"type":"pref_change","name":"muted_channels","value":"C2"}`)})
	if muted("C1") || !muted("C2") {
		t.Fatal("pref_change should move the mute to C2")
	}
	// A conversation put again keeps its mute.
	s.Apply(slack.Event{Type: "channel_joined", Raw: jsontext.Value(`{"channel":{"id":"C2","name":"dev"}}`)})
	if !muted("C2") {
		t.Fatal("a re-put conversation should keep its mute")
	}
}

func TestCollapseAndMove(t *testing.T) {
	s := New()
	s.ApplyBoot(boot())
	sections(s)
	if !s.Collapse("s1", true) || s.Collapse("nope", true) {
		t.Fatal("Collapse should know s1 and not nope")
	}
	s.Read(func(v View) {
		if !v.Sections()[1].Collapsed {
			t.Fatal("not collapsed")
		}
	})

	from, ok := s.Move("C1", "s0") // star it
	if !ok || from != "s1" {
		t.Fatalf("move: from %q ok %v", from, ok)
	}
	if got := names(s); !slices.Equal(got, []string{"Starred:C1", "Team:", "Channels:C2,G1"}) {
		t.Fatalf("after starring: %v", got)
	}
	s.Read(func(v View) {
		if v.SectionOf("C1") != "s0" || v.SectionOf("C2") != "" {
			t.Fatal("SectionOf wrong")
		}
	})
	if _, ok := s.Move("C1", "gone"); ok {
		t.Fatal("moving to a section that isn't there should fail")
	}
	// Back to where its kind puts it.
	if from, ok := s.Move("C1", ""); !ok || from != "s0" {
		t.Fatalf("unstar: %q %v", from, ok)
	}
	if got := names(s); !slices.Equal(got, []string{"Team:", "Channels:C1,C2,G1"}) {
		t.Fatalf("after unstarring: %v", got)
	}
}

func TestSectionEvents(t *testing.T) {
	s := New()
	s.ApplyBoot(boot())
	sections(s)
	ev := func(typ, body string) { s.Apply(slack.Event{Type: typ, Raw: jsontext.Value(body)}) }

	ev("channel_section_upserted", `{"channel_section_id":"s9","name":"Fresh","channel_section_type":"standard","next_channel_section_id":"s2","is_collapsed":true}`)
	ev("channel_sections_channels_upserted", `{"channel_section_id":"s9","channel_ids":["C2"]}`)
	var got []Section
	s.Read(func(v View) { got = v.Sections() })
	if len(got) != 4 || got[2].ID != "s9" || !got[2].Collapsed || !slices.Equal(got[2].Convs, []string{"C2"}) {
		t.Fatalf("after upsert: %+v", got)
	}
	ev("channel_sections_channels_removed", `{"channel_section_id":"s9","channel_ids":["C2"]}`)
	ev("channel_section_upserted", `{"channel_section_id":"s9","is_collapsed":false}`) // a bare one keeps the rest
	s.Read(func(v View) { got = v.Sections() })
	if got[2].Name != "Fresh" || got[2].Collapsed || len(got[2].Convs) != 0 {
		t.Fatalf("after remove and bare upsert: %+v", got[2])
	}
	ev("channel_section_deleted", `{"channel_section_id":"s9"}`)
	ev("channel_section_deleted", `{}`)
	s.Read(func(v View) { got = v.Sections() })
	if len(got) != 3 {
		t.Fatalf("after delete: %+v", got)
	}
}

func TestPreviewDropPut(t *testing.T) {
	s := New()
	s.ApplyBoot(boot())
	s.Preview(slack.Conversation{ID: "C7", Name: "bookclub", NumMembers: 7, Purpose: slack.Text{Value: "chapters"}})
	s.Read(func(v View) {
		c := v.Conv("C7")
		if c == nil || !c.Preview || c.Topic != "chapters" || len(v.Sidebar()[0].Convs) != 3 {
			t.Fatalf("preview %+v", c)
		}
	})
	s.Put(slack.Conversation{ID: "C7", Name: "bookclub"})
	s.Read(func(v View) {
		if c := v.Conv("C7"); c == nil || c.Preview || len(v.Sidebar()[0].Convs) != 4 {
			t.Fatalf("joined %+v", c)
		}
	})
	c, ok := s.Drop("C7")
	if !ok {
		t.Fatal("nothing dropped")
	}
	s.Read(func(v View) {
		if v.Conv("C7") != nil {
			t.Fatal("still there")
		}
	})
	s.Restore(c)
	s.Read(func(v View) {
		if v.Conv("C7") == nil {
			t.Fatal("not restored")
		}
	})
}

// DMs with nothing said yet tie, and must not swap places between frames.
func TestSidebarSteady(t *testing.T) {
	s := New()
	var b slack.UserBoot
	for i := range 30 {
		b.IMs = append(b.IMs, slack.Conversation{ID: fmt.Sprintf("D%02d", i), IsIM: true, User: fmt.Sprintf("U%02d", i)})
		b.Channels = append(b.Channels, slack.Conversation{ID: fmt.Sprintf("C%02d", i), Name: "same"})
	}
	s.ApplyBoot(b)
	var first []Section
	s.Read(func(v View) { first = v.Sidebar() })
	for range 20 {
		s.Read(func(v View) {
			if got := v.Sidebar(); !reflect.DeepEqual(got, first) {
				t.Fatalf("the sidebar moved:\n%v\n%v", first, got)
			}
		})
	}
}
