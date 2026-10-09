package slack

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// maxFetch is the most a picture's download may be.
const maxFetch = 32 << 20

// Fetch GETs a picture or file at u. Slack's own hosts (files.slack.com
// and the like) get the session's cookie, as the desktop app sends it;
// anyone else (slack-edge, gravatar) gets nothing. Go's client drops the
// cookie on a redirect off the host it was meant for.
func (c *Client) Fetch(ctx context.Context, u string) ([]byte, error) {
	began := time.Now()
	resp, err := c.get(ctx, c.http, u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxFetch))
	slog.Debug("fetch", "host", resp.Request.URL.Hostname(), "ms", time.Since(began).Milliseconds(), "bytes", len(b))
	return b, err
}

// Download is Fetch for a file someone asked for: streamed to w, so it can
// be as big as the disk, and with no clock on the whole of it.
func (c *Client) Download(ctx context.Context, u string, w io.Writer) (int64, error) {
	began := time.Now()
	resp, err := c.get(ctx, c.patient(), u)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	n, err := io.Copy(w, resp.Body)
	slog.Debug("download", "host", resp.Request.URL.Hostname(), "ms", time.Since(began).Milliseconds(), "bytes", n)
	return n, err
}

// patient is the client without its 30 s limit on a whole exchange, for
// bodies that are as long as they are. ctx still cancels them.
func (c *Client) patient() *http.Client {
	hc := *c.http
	hc.Timeout = 0
	return &hc
}

// get is the GET Fetch and Download share: the cookie for Slack's hosts,
// and an *Error for anything but 200.
func (c *Client) get(ctx context.Context, hc *http.Client, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	host := req.URL.Hostname()
	if req.URL.Scheme == "https" && slackHost(host) && c.creds.Cookie != "" {
		req.Header.Set("Cookie", c.cookie())
	}
	resp, err := hc.Do(req)
	if err != nil {
		slog.Warn("fetch", "host", host, "err", err.Error())
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		slog.Warn("fetch", "host", host, "status", resp.StatusCode)
		return nil, &Error{Method: "GET " + host, Status: resp.StatusCode}
	}
	return resp, nil
}

// slackHost is whether host is Slack's own, where the session cookie goes.
func slackHost(host string) bool {
	return host == "slack.com" || strings.HasSuffix(host, ".slack.com")
}
