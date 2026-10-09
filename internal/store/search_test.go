package store

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/jsonx"
)

// history is a fake conversations.history over n messages, ts 1000 up:
// newest first, filled from the latest end, as Slack does.
func history(t *testing.T, n int) *slack.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		latest, oldest := r.Form.Get("latest"), r.Form.Get("oldest")
		limit, _ := strconv.Atoi(r.Form.Get("limit"))
		var page []slack.Message
		more := false
		for i := n - 1; i >= 0; i-- {
			ts := fmt.Sprintf("%d.000000", 1000+i)
			if (latest != "" && ts > latest) || (oldest != "" && ts < oldest) {
				continue
			}
			if len(page) == limit {
				more = true
				break
			}
			page = append(page, slack.Message{TS: ts, User: "U1", Text: ts})
		}
		b, _ := jsonx.Marshal(map[string]any{"ok": true, "messages": page, "has_more": more})
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return slack.New(slack.Creds{URL: srv.URL, Token: "xoxc-t"})
}

func TestAround(t *testing.T) {
	ctx := context.Background()
	s := New()
	c := history(t, 200)
	if err := s.Open(ctx, c, "C1"); err != nil {
		t.Fatal(err)
	}
	// Far back: what's before it, and the window stops there.
	if err := s.Around(ctx, c, "C1", "1020.000000"); err != nil {
		t.Fatal(err)
	}
	s.Read(func(v View) {
		w := v.Window("C1")
		if !v.Holds("C1", "1020.000000") || !w.Newer || w.More || w.Msgs[len(w.Msgs)-1].TS != "1020.000000" || len(w.Msgs) != 21 {
			t.Fatalf("far back: %d held, newer %v more %v", len(w.Msgs), w.Newer, w.More)
		}
	})
	// What arrives meanwhile isn't added after a hole.
	s.Add("C1", slack.Message{TS: "1200.000000", User: "U1"})
	s.Read(func(v View) {
		if v.Holds("C1", "1200.000000") {
			t.Fatal("added past a hole")
		}
	})
	// Back to the newest, by Refresh's gap rule.
	if err := s.Newest(ctx, c, "C1"); err != nil {
		t.Fatal(err)
	}
	s.Read(func(v View) {
		w := v.Window("C1")
		if w.Newer || !w.More || v.Holds("C1", "1020.000000") || w.Msgs[len(w.Msgs)-1].TS != "1199.000000" {
			t.Fatalf("newest: %d held, newer %v more %v", len(w.Msgs), w.Newer, w.More)
		}
	})
	// Near the newest: both sides, and it reaches the end.
	if err := s.Around(ctx, c, "C1", "1180.000000"); err != nil {
		t.Fatal(err)
	}
	s.Read(func(v View) {
		w := v.Window("C1")
		if w.Newer || !w.More || len(w.Msgs) != 69 || w.Msgs[0].TS != "1131.000000" || w.Msgs[68].TS != "1199.000000" {
			t.Fatalf("near: %d held from %s, newer %v", len(w.Msgs), w.Msgs[0].TS, w.Newer)
		}
	})
}
