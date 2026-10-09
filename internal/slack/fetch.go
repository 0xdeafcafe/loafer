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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	host := req.URL.Hostname()
	if req.URL.Scheme == "https" && slackHost(host) && c.creds.Cookie != "" {
		req.Header.Set("Cookie", c.cookie())
	}
	began := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		slog.Warn("fetch", "host", host, "err", err.Error())
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("fetch", "host", host, "status", resp.StatusCode)
		return nil, &Error{Method: "GET " + host, Status: resp.StatusCode}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxFetch))
	slog.Debug("fetch", "host", host, "ms", time.Since(began).Milliseconds(), "bytes", len(b))
	return b, err
}

// slackHost is whether host is Slack's own, where the session cookie goes.
func slackHost(host string) bool {
	return host == "slack.com" || strings.HasSuffix(host, ".slack.com")
}
