package slack

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// The calls behind the message actions menu (docs/ui.md). Pins are the
// documented Web API methods. Saving is saved.add, whose outer shape was
// found (docs/slack-webapp-methods.md §4); what a duplicate answers is
// guessed.

// Pin pins the message at ts in channel, or unpins it. Pinning what's
// pinned, or unpinning what isn't, is an error Slack names (already_pinned,
// no_pin).
func (c *Client) Pin(ctx context.Context, channel, ts string, pin bool) error {
	method := "pins.remove"
	if pin {
		method = "pins.add"
	}
	return c.Call(ctx, method, url.Values{"channel": {channel}, "timestamp": {ts}}, nil)
}

// SaveMessage saves the message at ts in channel for later, due at the
// unix time due (0 for no time). It's what the web client's "remind me
// about this" does too: a saved item with a date_due.
// ponytail: date_due in seconds, and an "already_saved"-ish code on a
// duplicate, are guesses; a duplicate with a time becomes saved.update.
func (c *Client) SaveMessage(ctx context.Context, channel, ts string, due int64) error {
	f := url.Values{"item_type": {"message"}, "item_id": {channel}, "ts": {ts}}
	if due > 0 {
		f.Set("date_due", strconv.FormatInt(due, 10))
	}
	err := c.Call(ctx, "saved.add", f, nil)
	var e *Error
	if errors.As(err, &e) && strings.HasPrefix(e.Code, "already_") {
		if due > 0 {
			return c.Call(ctx, "saved.update", f, nil)
		}
		return nil
	}
	return err
}
