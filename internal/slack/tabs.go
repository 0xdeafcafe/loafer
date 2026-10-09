package slack

import (
	"context"
	"encoding/json/jsontext"
	"net/url"
	"strings"

	"github.com/0xdeafcafe/photon/jsonx"
)

// The calls behind the Activity and Later tabs, and the one message they
// point at. Method names are from Slack's web client; their outer shapes
// are as found (docs/slack-webapp-methods.md §2 and §4), and anything
// inside that wasn't is a best reading, decoded loosely: a field that
// isn't there is just empty.

// ActivityItem is one entry in activity.feed.
type ActivityItem struct {
	Key      string `json:"key"`
	FeedTS   string `json:"feed_ts"`
	IsUnread bool   `json:"is_unread"`
	Item     struct {
		Type     string        `json:"type"` // at_user, thread_v2, message_reaction, bot_dm_bundle, dm, ...
		Message  *ActivityNote `json:"message"`
		Reaction *struct {
			User string `json:"user"`
			Name string `json:"name"`
		} `json:"reaction"`
		// A thread's or app's entries come bundled. ponytail: guessed shape.
		Bundle *struct {
			Payload struct {
				Thread *struct {
					Channel  string `json:"channel_id"`
					ThreadTS string `json:"thread_ts"`
					Latest   string `json:"latest_ts"`
				} `json:"thread_entry"`
				Message *ActivityNote `json:"message"`
			} `json:"payload"`
		} `json:"bundle_info"`
	} `json:"item"`
}

// ActivityNote is the message an activity is about, as the feed names it.
// ponytail: guessed keys; the text may not come at all, so it's fetched.
type ActivityNote struct {
	TS       string `json:"ts"`
	Channel  string `json:"channel"`
	ThreadTS string `json:"thread_ts"`
	Author   string `json:"author_user_id"`
	User     string `json:"user"`
	Text     string `json:"text"`
}

// activityTypes is the web client's "all" filter, less invitations and
// the like, which Activity doesn't show.
var activityTypes = []string{"at_user", "at_user_group", "at_channel", "at_everyone", "keyword", "thread_v2", "message_reaction", "bot_dm_bundle", "dm"}

// Activity is the newest page of activity.feed, read and unread.
func (c *Client) Activity(ctx context.Context) ([]ActivityItem, error) {
	var r struct {
		Items []ActivityItem `json:"items"`
	}
	err := c.Call(ctx, "activity.feed", url.Values{
		"limit": {"30"},
		"mode":  {"chrono_reads_and_unreads"},
		// The web client hands FormData an array, which joins it with commas.
		"types": {strings.Join(activityTypes, ",")},
	}, &r)
	return r.Items, err
}

// MarkActivityRead marks one entry read. ponytail: the wire keys weren't
// found; these are the client's item fields, snake_cased, as it sends them.
func (c *Client) MarkActivityRead(ctx context.Context, key, feedTS, typ, channel, ts string) error {
	return c.Call(ctx, "activity.markRead", url.Values{
		"key": {key}, "feed_ts": {feedTS}, "type": {typ}, "channel_id": {channel}, "message_ts": {ts},
	}, nil)
}

// SavedItem is one entry in saved.list.
type SavedItem struct {
	Type          string   `json:"item_type"` // message, file, reminder, ...
	ID            string   `json:"item_id"`   // the conversation, for a message
	TS            string   `json:"ts"`
	DateDue       int64    `json:"date_due"`
	DateCompleted int64    `json:"date_completed"`
	IsArchived    bool     `json:"is_archived"`
	State         string   `json:"state"`   // in_progress, completed, archived
	Message       *Message `json:"message"` // ponytail: in case it comes; else fetched
}

// Saved is what's saved for later and still in progress.
// ponytail: one page, of what Slack sends by default; the response key
// and paging weren't found, so both likely keys are read.
func (c *Client) Saved(ctx context.Context) ([]SavedItem, error) {
	var r struct {
		Saved []SavedItem `json:"saved_items"`
		Items []SavedItem `json:"items"`
	}
	err := c.Call(ctx, "saved.list", url.Values{"filter": {"saved"}, "limit": {"50"}}, &r)
	if len(r.Saved) == 0 {
		r.Saved = r.Items
	}
	return r.Saved, err
}

// CompleteSaved marks a saved item done. ponytail: mark=completed is the
// pair of the uncompleted that was found.
func (c *Client) CompleteSaved(ctx context.Context, typ, id, ts string) error {
	return c.Call(ctx, "saved.update", url.Values{"item_type": {typ}, "item_id": {id}, "ts": {ts}, "mark": {"completed"}}, nil)
}

// Unsave takes an item out of Later altogether.
func (c *Client) Unsave(ctx context.Context, typ, id, ts string) error {
	return c.Call(ctx, "saved.delete", url.Values{"item_type": {typ}, "item_id": {id}, "ts": {ts}}, nil)
}

// Message fetches the message at ts in channel; a reply when threadTS is
// its thread's.
func (c *Client) Message(ctx context.Context, channel, ts, threadTS string) (Message, error) {
	method := "conversations.history"
	f := url.Values{"channel": {channel}, "oldest": {ts}, "latest": {ts}, "inclusive": {"true"}, "limit": {"1"}}
	if threadTS != "" && threadTS != ts {
		// replies leads with the parent, so ask for two.
		method = "conversations.replies"
		f.Set("ts", threadTS)
		f.Set("limit", "2")
	}
	var r struct {
		Messages []Message `json:"messages"`
	}
	if err := c.Call(ctx, method, f, &r); err != nil {
		return Message{}, err
	}
	for _, m := range r.Messages {
		if m.TS == ts {
			return m, nil
		}
	}
	return Message{}, &Error{Method: method, Code: "message_not_found"}
}

// Badge reads a count that's a number in some payloads and an object of
// counts in others. ponytail: client.counts' activity_v2 wasn't seen;
// this takes a number, or the first count it knows in an object.
func Badge(v jsontext.Value) int {
	switch v.Kind() {
	case '0':
		var n int
		_ = jsonx.Unmarshal(v, &n)
		return n
	case '{':
		var o struct {
			Unread int `json:"unread_count"`
			Total  int `json:"total_unread_count"`
			Badge  int `json:"badge_count"`
			Count  int `json:"count"`
		}
		_ = jsonx.Unmarshal(v, &o)
		return max(o.Unread, o.Total, o.Badge, o.Count)
	}
	return 0
}
