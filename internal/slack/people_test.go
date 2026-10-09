package slack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestWatch(t *testing.T) {
	frames := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"app.slack.com"}})
		if err != nil {
			return
		}
		defer c.CloseNow()
		_ = c.Write(r.Context(), websocket.MessageText, []byte(`{"type":"hello"}`))
		for {
			_, b, err := c.Read(r.Context())
			if err != nil {
				return
			}
			frames <- string(b)
		}
	}))
	defer srv.Close()
	defer func(g string) { Gateway = g }(Gateway)
	Gateway = "ws" + strings.TrimPrefix(srv.URL, "http") + "/"

	c := New(Creds{TeamID: "T1", Token: "xoxc-1", Cookie: "xoxd-2"})
	c.Watch([]string{"U1", ""}) // before there's a socket: sent when there is
	listen := func() (stop func()) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			_ = c.Listen(ctx, "", func(Event) error { return nil })
			close(done)
		}()
		return func() { cancel(); <-done }
	}
	want := func(ids string) {
		t.Helper()
		select {
		case got := <-frames:
			if exp := `{"type":"presence_sub","ids":[` + ids + `]}`; got != exp {
				t.Fatalf("frame %s, want %s", got, exp)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no frame for %s", ids)
		}
	}

	stop := listen()
	want(`"U1"`)

	// Calls close together share a frame, and what's been asked is not asked again.
	c.Watch([]string{"U1", "U2"})
	c.Watch([]string{"U3"})
	want(`"U2","U3"`)
	select {
	case got := <-frames:
		t.Fatalf("a second frame: %s", got)
	case <-time.After(3 * presenceDelay):
	}

	// A new socket knows nothing, so it's told everyone.
	stop()
	defer listen()()
	want(`"U1","U2","U3"`)
}
