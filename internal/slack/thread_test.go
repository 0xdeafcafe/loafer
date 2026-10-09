package slack

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Replies follows the cursor, and a parent sent again on a later page is
// had once.
func TestReplies(t *testing.T) {
	var posted url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f, _ := url.ParseQuery(string(b))
		switch {
		case r.URL.Path == "/api/chat.postMessage":
			posted = f
			io.WriteString(w, `{"ok":true,"message":{"ts":"3.0","thread_ts":"1.0","subtype":"thread_broadcast"}}`)
		case f.Get("channel") != "C1" || f.Get("ts") != "1.0":
			io.WriteString(w, `{"ok":false,"error":"thread_not_found"}`)
		case f.Get("cursor") == "":
			io.WriteString(w, `{"ok":true,"messages":[{"ts":"1.0","reply_count":2},{"ts":"1.5"}],"response_metadata":{"next_cursor":"p2"}}`)
		default:
			io.WriteString(w, `{"ok":true,"messages":[{"ts":"1.0","reply_count":2},{"ts":"2.0"}],"response_metadata":{"next_cursor":""}}`)
		}
	}))
	defer srv.Close()
	c := New(Creds{URL: srv.URL, Token: "xoxc-t", Cookie: "xoxd-c"})

	msgs, err := c.Replies(context.Background(), "C1", "1.0")
	var got []string
	for _, m := range msgs {
		got = append(got, m.TS)
	}
	if err != nil || strings.Join(got, ",") != "1.0,1.5,2.0" {
		t.Fatalf("replies %v: %v", got, err)
	}

	m, err := c.Reply(context.Background(), "C1", "1.0", "hi", true)
	if err != nil || m.TS != "3.0" || posted.Get("thread_ts") != "1.0" || posted.Get("reply_broadcast") != "true" {
		t.Fatalf("reply %+v %v, posted %v", m, err, posted)
	}
}
