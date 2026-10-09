package slack

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/0xdeafcafe/loafer/internal/obs"
	"github.com/0xdeafcafe/photon/jsonx"
)

// Event is one frame from the websocket. Its shape depends on its type,
// so the rest stays raw for whoever applies it.
type Event struct {
	Type    string         `json:"type"`
	Subtype string         `json:"subtype"`
	Raw     jsontext.Value `json:"-"`
}

// Gateway is the address the desktop app opens its websocket on, where a
// session token needs no rtm.connect first (docs/slack-internal-api.md).
var Gateway = "wss://wss-primary.slack.com/"

// How quiet the socket may go: a ping after idle, given up on after dead.
var (
	idle = 30 * time.Second
	dead = 75 * time.Second
)

// Listen opens the websocket (at to, a reconnect_url, when it's set) and
// calls on with each event until ctx ends, the socket drops, or on
// returns an error, which is what it returns.
func (c *Client) Listen(ctx context.Context, to string, on func(Event) error) error {
	if to == "" {
		q := url.Values{
			"token":                {c.creds.Token},
			"gateway_server":       {c.creds.TeamID + "-1"},
			"slack_client":         {"desktop"},
			"batch_presence_aware": {"1"},
		}
		to = c.gateway + "?" + q.Encode()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, to, &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": {c.cookie()}, "Origin": {"https://app.slack.com"}},
	})
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(8 << 20) // a message with blocks and files runs past the 32 KB default

	// Reads only end with the socket, so a quiet one is pinged from here,
	// and closed when even that brings nothing back.
	var heard atomic.Int64
	heard.Store(time.Now().UnixNano())
	go func() {
		t := time.NewTicker(idle / 3)
		defer t.Stop()
		for id := 1; ; {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			switch quiet := time.Since(time.Unix(0, heard.Load())); {
			case quiet >= dead:
				slog.Warn("ws", "err", "no reply", "quiet_s", int(quiet.Seconds()))
				conn.CloseNow()
				return
			case quiet >= idle:
				_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"ping","id":`+strconv.Itoa(id)+`}`))
				id++
			}
		}
	}()

	for {
		_, b, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		heard.Store(time.Now().UnixNano())
		var ev Event
		if err := jsonx.Unmarshal(b, &ev); err != nil {
			slog.Warn("ws", "err", "bad json", "bytes", len(b))
			continue
		}
		ev.Raw = b
		if obs.Tracing(obs.WS) {
			slog.Debug("ws.event", "body", string(b))
		}
		if ev.Type == "error" {
			return errors.New("websocket: " + string(b))
		}
		if err := on(ev); err != nil {
			return err
		}
	}
}

// cookie is the d cookie as the web client sends it.
func (c *Client) cookie() string {
	return "d=" + c.creds.Cookie + "; d-s=" + strconv.FormatInt(time.Now().Unix()-10, 10)
}
