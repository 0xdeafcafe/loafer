// Package slack talks to Slack the way its desktop app does (and, for
// loafer's own Slack app, the way any app does): a session
// token (xoxc-) in the form and the d cookie, against the workspace's own
// host. Every call is logged (method, time, size, outcome) and, while API
// tracing is on, its body too; obs redacts both.
package slack

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/0xdeafcafe/loafer/internal/obs"
	"github.com/0xdeafcafe/photon/jsonx"
)

// Creds sign in to one workspace.
type Creds struct {
	TeamID string `json:"team_id"`
	Team   string `json:"team"` // display name
	URL    string `json:"url"`  // https://<domain>.slack.com/
	UserID string `json:"user_id"`
	Token  string `json:"token"`  // xoxc-…
	Cookie string `json:"cookie"` // the d cookie's value, as the browser keeps it (xoxd-…, URL-encoded)
}

// Client calls one workspace's API.
type Client struct {
	base  string // https://<domain>.slack.com/api/
	creds Creds
	http  *http.Client
	pres  presence // who the websocket is asked about (people.go)
}

var inflight = obs.Gauge("api.inflight")

// New makes a client for c. An empty c.URL uses slack.com, which is enough
// for auth.test to find the workspace's own.
func New(c Creds) *Client {
	base := strings.TrimSuffix(c.URL, "/")
	if base == "" {
		base = "https://slack.com"
	}
	return &Client{base: base + "/api/", creds: c, http: &http.Client{Timeout: 30 * time.Second}}
}

// Error is Slack's ok:false, or an HTTP failure.
type Error struct {
	Method     string
	Code       string        // Slack's "error", e.g. invalid_auth
	Status     int           // HTTP status when it wasn't 200
	RetryAfter time.Duration // set on 429
}

func (e *Error) Error() string {
	if e.Code != "" {
		return e.Method + ": " + e.Code
	}
	return fmt.Sprintf("%s: HTTP %d", e.Method, e.Status)
}

// SignedOut says whether err means the token or cookie stopped working.
func SignedOut(err error) bool {
	var e *Error
	return errors.As(err, &e) && (e.Code == "invalid_auth" || e.Code == "not_authed" || e.Code == "token_revoked" || e.Code == "account_inactive")
}

// Call posts form to method and decodes the response into out (which may
// be nil). ok:false comes back as *Error.
func (c *Client) Call(ctx context.Context, method string, form url.Values, out any) error {
	if form == nil {
		form = url.Values{}
	}
	session := strings.HasPrefix(c.creds.Token, "xoxc-")
	if session {
		form.Set("token", c.creds.Token)
	}
	body := form.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+method, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if session {
		req.Header.Set("Cookie", c.cookie())
	} else {
		// A real app's tokens (xoxb-, xapp-) go as a bearer, with no cookie.
		req.Header.Set("Authorization", "Bearer "+c.creds.Token)
	}

	inflight.Add(1)
	began := time.Now()
	resp, err := c.http.Do(req)
	var b []byte
	if err == nil {
		b, err = io.ReadAll(resp.Body)
		resp.Body.Close()
	}
	inflight.Add(-1)
	took := time.Since(began)

	attrs := []any{"method", method, "ms", took.Milliseconds(), "bytes", len(b)}
	if obs.Tracing(obs.API) {
		slog.Debug("api.req", "method", method, "body", body)
		slog.Debug("api.resp", "method", method, "body", string(b))
	}
	if err != nil {
		slog.Warn("api", append(attrs, "err", err.Error())...)
		return err
	}
	if resp.StatusCode != http.StatusOK {
		e := &Error{Method: method, Status: resp.StatusCode}
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			e.RetryAfter = time.Duration(s) * time.Second
		}
		slog.Warn("api", append(attrs, "status", resp.StatusCode, "retry_after", e.RetryAfter.Seconds())...)
		return e
	}
	var head struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := jsonx.Unmarshal(b, &head); err != nil {
		slog.Warn("api", append(attrs, "err", "bad json")...)
		return fmt.Errorf("%s: %w", method, err)
	}
	if !head.OK {
		slog.Warn("api", append(attrs, "error", head.Error)...)
		return &Error{Method: method, Code: head.Error}
	}
	slog.Info("api", attrs...)
	if out == nil || bytes.Equal(b, []byte(`{"ok":true}`)) {
		return nil
	}
	return jsonx.Unmarshal(b, out)
}

// AuthTest checks the creds and says who and where they're for.
func (c *Client) AuthTest(ctx context.Context) (Creds, error) {
	var r struct {
		URL    string `json:"url"`
		Team   string `json:"team"`
		TeamID string `json:"team_id"`
		UserID string `json:"user_id"`
	}
	if err := c.Call(ctx, "auth.test", nil, &r); err != nil {
		return Creds{}, err
	}
	got := c.creds
	got.URL, got.Team, got.TeamID, got.UserID = r.URL, r.Team, r.TeamID, r.UserID
	return got, nil
}
