package store

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

// tabServer answers the tabs' calls with what the web client was seen to
// get, as near as is known.
func tabServer(t *testing.T) *slack.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f, _ := url.ParseQuery(string(b))
		switch r.URL.Path {
		case "/api/activity.feed":
			io.WriteString(w, `{"ok":true,"items":[
				{"key":"k1","feed_ts":"9.0","is_unread":true,"item":{"type":"at_user","message":{"ts":"5.0","channel":"C1","author_user_id":"U1"}}},
				{"key":"k2","feed_ts":"8.0","is_unread":false,"item":{"type":"message_reaction","message":{"ts":"4.0","channel":"C1","text":"mine"},"reaction":{"user":"U1","name":"tada"}}},
				{"key":"k3","feed_ts":"7.0","is_unread":true,"item":{"type":"thread_v2","bundle_info":{"payload":{"thread_entry":{"channel_id":"C1","thread_ts":"2.0","latest_ts":"3.0"}}}}}]}`)
		case "/api/saved.list":
			io.WriteString(w, `{"ok":true,"saved_items":[
				{"item_type":"message","item_id":"C1","ts":"5.0","date_due":1700000000,"state":"in_progress","todo_state":"saved"},
				{"item_type":"message","item_id":"C1","ts":"4.0","date_due":null,"date_completed":1,"state":"completed"}]}`)
		case "/api/conversations.history":
			if f.Get("channel") == "D1" && f.Get("limit") == "1" && f.Get("latest") == "" {
				io.WriteString(w, `{"ok":true,"messages":[{"ts":"6.0","user":"U1","text":"latest in the dm"}]}`)
				return
			}
			if f.Get("latest") == "5.0" && f.Get("oldest") == "5.0" {
				io.WriteString(w, `{"ok":true,"messages":[{"ts":"5.0","user":"U1","text":"hey <@U0>"}]}`)
				return
			}
			io.WriteString(w, `{"ok":true,"messages":[]}`)
		case "/api/conversations.replies":
			if f.Get("ts") == "2.0" {
				io.WriteString(w, `{"ok":true,"messages":[{"ts":"2.0","text":"parent"},{"ts":"3.0","user":"U2","text":"a reply"}]}`)
				return
			}
			io.WriteString(w, `{"ok":true,"messages":[]}`)
		default:
			io.WriteString(w, `{"ok":false,"error":"unknown_method"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return slack.New(slack.Creds{URL: srv.URL, Token: "xoxc-t", Cookie: "c"})
}

func TestActivity(t *testing.T) {
	s, c, ctx := New(), tabServer(t), context.Background()
	s.ApplyBoot(boot())
	s.ApplyCounts(slack.Counts{Activity: []byte(`{"unread_count":4}`)})
	s.Apply(ev(t, `{"type":"activity"}`))
	s.Read(func(v View) {
		if v.Badge(ActivityList) != 4 || v.Stale(ActivityList) {
			t.Fatalf("before a fetch: badge %d stale %v", v.Badge(ActivityList), v.Stale(ActivityList))
		}
	})
	if err := s.Fetch(ctx, c, ActivityList); err != nil {
		t.Fatal(err)
	}
	s.Read(func(v View) {
		as := v.Activity()
		if len(as) != 3 || v.Badge(ActivityList) != 2 {
			t.Fatalf("%d items, badge %d", len(as), v.Badge(ActivityList))
		}
		if a := as[0]; a.Msg.Text != "hey <@U0>" || a.Conv != "C1" || !a.Unread {
			t.Errorf("a mention is fetched: %+v", a)
		}
		if a := as[1]; a.Msg.Text != "mine" || a.Reactor != "U1" || a.Reaction != "tada" {
			t.Errorf("a reaction keeps who and which: %+v", a)
		}
		if a := as[2]; a.Msg.Text != "a reply" || a.ThreadTS != "2.0" || a.TS != "3.0" {
			t.Errorf("a thread's reply is fetched from its thread: %+v", a)
		}
	})
	s.ReadActivity("k1")
	s.Apply(ev(t, `{"type":"activity","whatever":1}`))
	s.Read(func(v View) {
		if v.Badge(ActivityList) != 1 || !v.Stale(ActivityList) {
			t.Fatalf("after: badge %d stale %v", v.Badge(ActivityList), v.Stale(ActivityList))
		}
	})
}

func TestLater(t *testing.T) {
	s, c, ctx := New(), tabServer(t), context.Background()
	s.ApplyBoot(boot())
	s.ApplyCounts(slack.Counts{})
	if err := s.Fetch(ctx, c, LaterList); err != nil {
		t.Fatal(err)
	}
	s.Read(func(v View) {
		xs := v.Saved()
		if len(xs) != 1 || v.Badge(LaterList) != 1 {
			t.Fatalf("completed ones should go: %+v", xs)
		}
		if x := xs[0]; x.Conv != "C1" || x.TS != "5.0" || x.Due != 1700000000 || x.Msg.Text != "hey <@U0>" {
			t.Fatalf("saved: %+v", x)
		}
	})
	s.Apply(ev(t, `{"type":"saved_updated","saved":{}}`))
	s.Unsave("C1", "5.0")
	s.Read(func(v View) {
		if len(v.Saved()) != 0 || !v.Stale(LaterList) {
			t.Fatalf("unsave: %+v stale %v", v.Saved(), v.Stale(LaterList))
		}
	})
}

func TestDMs(t *testing.T) {
	s, c, ctx := New(), tabServer(t), context.Background()
	s.ApplyBoot(boot())
	s.ApplyCounts(slack.Counts{IMs: []slack.Snapshot{{ID: "D1", Latest: "6.0"}, {ID: "D2", Latest: "1.0"}}, MPIMs: []slack.Snapshot{{ID: "M1", Latest: "3.0"}}})
	if err := s.Fetch(ctx, c, DMList); err != nil {
		t.Fatal(err)
	}
	s.Read(func(v View) {
		if got := v.DMs(); len(got) != 3 || got[0] != "D1" || got[1] != "M1" {
			t.Fatalf("newest first: %v", got)
		}
		if m, ok := v.LastMsg("D1"); !ok || m.Text != "latest in the dm" {
			t.Fatalf("preview: %+v", m)
		}
	})
	s.Apply(ev(t, `{"type":"message","channel":"D2","ts":"7.0","user":"B1","text":"beep"}`))
	s.Read(func(v View) {
		if m, _ := v.LastMsg("D2"); m.Text != "beep" || v.DMs()[0] != "D2" || v.Badge(DMList) != 1 {
			t.Fatalf("a new message tops the list: %v %+v badge %d", v.DMs(), m, v.Badge(DMList))
		}
	})
}
