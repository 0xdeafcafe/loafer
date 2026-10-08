package store

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

func boot() slack.UserBoot {
	var b slack.UserBoot
	b.Self.ID, b.Self.Name = "U0", "alex"
	b.Channels = []slack.Conversation{
		{ID: "C2", Name: "dev", IsChannel: true, IsMember: true},
		{ID: "C1", Name: "alerts", IsChannel: true, IsMember: true},
		{ID: "G1", Name: "secret", IsGroup: true, IsPrivate: true, IsMember: true},
		{ID: "C9", Name: "old", IsChannel: true, IsMember: true, IsArchived: true},
		{ID: "M1", Name: "mpdm-alex--drew--sergio-1", IsMPIM: true},
	}
	b.IMs = []slack.Conversation{{ID: "D1", User: "U1", IsIM: true}, {ID: "D2", User: "B1", IsIM: true}}
	return b
}

func TestSidebarFallback(t *testing.T) {
	s := New()
	s.ApplyBoot(boot())
	s.ApplyPeople([]slack.User{{ID: "U1", Name: "drew"}, {ID: "B1", Name: "grafana", IsBot: true}})
	s.ApplyCounts(slack.Counts{IMs: []slack.Snapshot{{ID: "D1", Latest: "2.0"}}, MPIMs: []slack.Snapshot{{ID: "M1", Latest: "3.0"}}})
	var got []string
	s.Read(func(v View) {
		for _, sec := range v.Sidebar() {
			got = append(got, fmt.Sprint(sec.Name, sec.Convs))
		}
		if v.Title(v.Conv("M1")) != "drew, sergio" || v.Title(v.Conv("D1")) != "drew" {
			t.Errorf("titles: %q %q", v.Title(v.Conv("M1")), v.Title(v.Conv("D1")))
		}
	})
	want := []string{"Channels[C1 C2 G1]", "Direct messages[M1 D1]", "Apps[D2]"}
	if !slices.Equal(got, want) {
		t.Fatalf("sidebar %v, want %v", got, want)
	}
}

// Slack's sections come in linked order, and catch-all sections take
// only what custom ones left.
func TestSidebarSections(t *testing.T) {
	s := New()
	s.ApplyBoot(boot())
	mk := func(id, name, typ, next string, ids ...string) slack.Section {
		x := slack.Section{ID: id, Name: name, Type: typ, Next: next}
		x.Channels.IDs = ids
		return x
	}
	s.ApplySections([]slack.Section{
		mk("s3", "Channels", "channels", ""),
		mk("s1", "big bong", "standard", "s2", "C1"),
		mk("s2", "empty", "standard", "s3"),
	})
	var got []string
	s.Read(func(v View) {
		for _, sec := range v.Sidebar() {
			got = append(got, fmt.Sprint(sec.Name, sec.Convs))
		}
	})
	want := []string{"big bong[C1]", "empty[]", "Channels[C2 G1]"}
	if !slices.Equal(got, want) {
		t.Fatalf("sidebar %v, want %v", got, want)
	}
}

func TestWindowsAndCache(t *testing.T) {
	s := New()
	s.ApplyBoot(boot())
	for i := range keepWindows + 3 {
		s.SetWindow(fmt.Sprint("C", i), []slack.Message{{TS: "2"}, {TS: "1"}}, true, false)
	}
	s.SetWindow("C22", []slack.Message{{TS: "0"}}, false, true)
	s.Read(func(v View) {
		if len(v.s.windows) != keepWindows || v.Window("C0") != nil {
			t.Fatalf("kept %d windows, C0 kept: %v", len(v.s.windows), v.Window("C0") != nil)
		}
		w := v.Window("C22")
		if len(w.Msgs) != 3 || w.Msgs[0].TS != "0" || w.Msgs[2].TS != "2" || w.More {
			t.Fatalf("window C22: %+v", w)
		}
	})

	path := filepath.Join(t.TempDir(), "state.json")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	r := New()
	if err := r.Load(path); err != nil {
		t.Fatal(err)
	}
	r.Read(func(v View) {
		if v.Self() != "U0" || v.Conv("C1") == nil || v.Window("C22") == nil || len(v.Sidebar()) == 0 {
			t.Fatal("cache didn't round trip")
		}
	})
}
