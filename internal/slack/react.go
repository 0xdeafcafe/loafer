package slack

import (
	"context"
	"net/url"
)

// React adds your reaction name to the message at ts, or takes it away.
// Adding one that's there, or taking away one that isn't, is an error
// Slack names (already_reacted, no_reaction).
func (c *Client) React(ctx context.Context, channel, ts, name string, add bool) error {
	method := "reactions.remove"
	if add {
		method = "reactions.add"
	}
	return c.Call(ctx, method, url.Values{"channel": {channel}, "timestamp": {ts}, "name": {name}}, nil)
}
