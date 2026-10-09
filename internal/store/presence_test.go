package store

import (
	"testing"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

func presence(s *Store, id string) (p string) {
	s.Read(func(v View) { p = v.Presence(id) })
	return p
}

func TestPresence(t *testing.T) {
	s := New()
	s.self = "UME"
	s.ApplyPeople([]slack.User{{ID: "U1", Name: "drew"}})
	names, version := s.names, s.Version()

	s.Apply(ev(t, `{"type":"presence_change","user":"U1","presence":"active"}`))
	s.Apply(ev(t, `{"type":"presence_change","users":["U2","U3"],"presence":"away"}`)) // the batch form
	s.Apply(ev(t, `{"type":"manual_presence_change","presence":"away"}`))
	s.Apply(ev(t, `{"type":"presence_change","user":"U1","presence":"sideways"}`)) // nothing Slack says
	for id, want := range map[string]string{"U1": "active", "U2": "away", "U3": "away", "UME": "away", "U4": ""} {
		if got := presence(s, id); got != want {
			t.Errorf("%s is %q, want %q", id, got, want)
		}
	}
	if s.names != names {
		t.Fatal("presence must not make drawn messages draw again")
	}
	if s.Version() != version+3 {
		t.Fatalf("version %d, want %d", s.Version(), version+3)
	}

	// Told the same again, nothing has changed, so nobody is woken.
	version = s.Version()
	s.Apply(ev(t, `{"type":"presence_change","users":["U2","U3"],"presence":"away"}`))
	if s.Version() != version {
		t.Fatal("the same presence twice should not be a change")
	}
}

func TestApplyUser(t *testing.T) {
	s := New()
	s.ApplyPeople([]slack.User{{ID: "U1", Name: "drew", RealName: "Drew Dunn"}})
	names := s.names
	var u slack.User
	u.ID, u.Name, u.RealName, u.TZ = "U1", "drew", "Drew Dunn", "Europe/London"
	u.Profile.Title, u.Profile.Email = "Chef", "drew@example.com"
	s.ApplyUser(u)
	if s.names != names {
		t.Fatal("a new title is not on any drawn message")
	}
	if p := *s.people["U1"]; p.Title != "Chef" || p.Email != "drew@example.com" || p.TZ != "Europe/London" || p.Real != "Drew Dunn" {
		t.Fatalf("profile: %+v", p)
	}

	s.Apply(ev(t, `{"type":"user_change","user":{"id":"U1","name":"drew","real_name":"Drew Dunn","profile":{"title":"Chef","status_emoji":":palm_tree:","status_text":"away","status_expiration":9999999999}}}`))
	if s.names != names+1 || s.people["U1"].StatusEmoji != ":palm_tree:" || s.people["U1"].StatusUntil != 9999999999 {
		t.Fatalf("a status is on drawn messages: names %d, %+v", s.names, *s.people["U1"])
	}

	// A status alone changes only the status.
	s.Apply(ev(t, `{"type":"user_status_changed","user":{"id":"U1","profile":{"status_emoji":"","status_text":""}}}`))
	if p := *s.people["U1"]; p.StatusEmoji != "" || p.Name != "Drew Dunn" || p.Handle != "drew" || p.Title != "Chef" {
		t.Fatalf("a cleared status: %+v", p)
	}
	if s.names != names+2 {
		t.Fatalf("clearing it redraws: names %d", s.names)
	}
}

func TestGroups(t *testing.T) {
	s := New()
	s.ApplyGroups([]slack.Group{{ID: "S1", Handle: "bakers", Name: "Bakers"}, {ID: "S2", Handle: "gone", Deleted: 5}})
	group := func(id string) (g Group, ok bool) {
		s.Read(func(v View) { g, ok = v.Group(id) })
		return g, ok
	}
	if g, ok := group("S1"); !ok || g.Handle != "bakers" {
		t.Fatalf("S1: %+v %v", g, ok)
	}
	if _, ok := group("S2"); ok {
		t.Fatal("a deleted group is not one")
	}
	names := s.names
	s.Apply(ev(t, `{"type":"subteam_created","subteam":{"id":"S3","handle":"pastry","name":"Pastry"}}`))
	s.Apply(ev(t, `{"type":"subteam_updated","subteam":{"id":"S1","handle":"loaves"}}`)) // the name carries on
	if g, _ := group("S1"); g.Handle != "loaves" || g.Name != "Bakers" {
		t.Fatalf("updated: %+v", g)
	}
	if g, ok := group("S3"); !ok || g.Name != "Pastry" {
		t.Fatalf("created: %+v %v", g, ok)
	}
	s.Apply(ev(t, `{"type":"subteam_updated","subteam":{"id":"S3","date_delete":1700000000}}`))
	if _, ok := group("S3"); ok {
		t.Fatal("a group deleted by update is gone")
	}
	if s.names != names+3 {
		t.Fatalf("groups are drawn in messages: names %d", s.names)
	}
}
