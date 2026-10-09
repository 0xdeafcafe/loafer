package slack

import (
	"context"
	"net/url"
	"slices"
)

// maxReplies is the most of a thread Replies fetches.
// shortcut: a thread past it shows its oldest 1000; page from the end
// (latest=) if threads that long turn up.
const maxReplies = 1000

// Replies is the thread at ts in channel: its parent, then the replies,
// oldest first, as conversations.replies gives them.
func (c *Client) Replies(ctx context.Context, channel, ts string) ([]Message, error) {
	var out []Message
	cursor := ""
	for {
		f := url.Values{"channel": {channel}, "ts": {ts}, "limit": {"200"}}
		if cursor != "" {
			f.Set("cursor", cursor)
		}
		var r struct {
			Messages []Message `json:"messages"`
			Meta     struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := c.Call(ctx, "conversations.replies", f, &r); err != nil {
			return nil, err
		}
		if cursor != "" {
			// Later pages may lead with the parent again; it's had.
			r.Messages = slices.DeleteFunc(r.Messages, func(m Message) bool { return m.TS == ts })
		}
		out = append(out, r.Messages...)
		if cursor = r.Meta.NextCursor; cursor == "" || len(out) >= maxReplies {
			return out, nil
		}
	}
}

// Reply sends text to the thread at threadTS in channel, and to the
// channel too when broadcast is set, and returns the reply as Slack
// stored it.
func (c *Client) Reply(ctx context.Context, channel, threadTS, text string, broadcast bool) (Message, error) {
	f := url.Values{"channel": {channel}, "text": {text}, "link_names": {"true"}, "thread_ts": {threadTS}}
	if broadcast {
		f.Set("reply_broadcast", "true")
	}
	var r struct {
		Message Message `json:"message"`
	}
	err := c.Call(ctx, "chat.postMessage", f, &r)
	return r.Message, err
}

// MarkThread moves the thread at threadTS's read marker to ts. The web
// client's own call; its keys are wee-slack's (docs/slack-internal-api.md)
// and unconfirmed against a capture.
func (c *Client) MarkThread(ctx context.Context, channel, threadTS, ts string) error {
	return c.Call(ctx, "subscriptions.thread.mark", url.Values{"channel": {channel}, "thread_ts": {threadTS}, "ts": {ts}, "read": {"1"}}, nil)
}
