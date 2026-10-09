package slack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

func TestListen(t *testing.T) {
	var cookie, token string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, token = r.Header.Get("Cookie"), r.URL.Query().Get("token")
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"app.slack.com"}})
		if err != nil {
			return
		}
		defer c.CloseNow()
		for _, f := range []string{`{"type":"hello"}`, `not json`, `{"type":"message","channel":"C1","ts":"1.0"}`} {
			_ = c.Write(r.Context(), websocket.MessageText, []byte(f))
		}
		_, _, _ = c.Read(r.Context()) // until the client goes
	}))
	defer srv.Close()
	defer func(g string) { Gateway = g }(Gateway)
	Gateway = "ws" + strings.TrimPrefix(srv.URL, "http") + "/"

	c := New(Creds{TeamID: "T1", Token: "xoxc-1", Cookie: "xoxd-2"})
	var got []string
	stop := errors.New("stop")
	err := c.Listen(context.Background(), "", func(ev Event) error {
		got = append(got, ev.Type)
		if ev.Type == "message" {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("Listen should end with on's error: %v", err)
	}
	if strings.Join(got, ",") != "hello,message" {
		t.Fatalf("events: %v", got)
	}
	if token != "xoxc-1" || !strings.HasPrefix(cookie, "d=xoxd-2;") {
		t.Fatalf("handshake: token %q cookie %q", token, cookie)
	}
}
