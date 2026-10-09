package slack

import (
	"context"
	"net/url"
	"strings"

	"github.com/0xdeafcafe/photon/jsonx"
)

// Finding, joining and leaving conversations, and keeping the sidebar.
// The conversations.* methods are Slack's documented ones, which take a
// session token. users.prefs.set and users.channelSections.* are the web
// client's: the method names are found in its bundle, the arguments below
// are guesses (docs/slack-webapp-methods.md, Managing the sidebar).

// Browse is a page of the public channels in the workspace that aren't
// archived, yours and others', from conversations.list. cursor is "" for
// the first; next is "" after the last.
func (c *Client) Browse(ctx context.Context, cursor string) (chans []Conversation, next string, err error) {
	f := url.Values{"types": {"public_channel"}, "exclude_archived": {"true"}, "limit": {"200"}}
	if cursor != "" {
		f.Set("cursor", cursor)
	}
	var r struct {
		Channels []Conversation `json:"channels"`
		Meta     struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	err = c.Call(ctx, "conversations.list", f, &r)
	return r.Channels, r.Meta.NextCursor, err
}

// Join joins a public channel.
func (c *Client) Join(ctx context.Context, id string) (Conversation, error) {
	var r struct {
		Channel Conversation `json:"channel"`
	}
	err := c.Call(ctx, "conversations.join", url.Values{"channel": {id}}, &r)
	return r.Channel, err
}

// Leave leaves a channel.
func (c *Client) Leave(ctx context.Context, id string) error {
	return c.Call(ctx, "conversations.leave", url.Values{"channel": {id}}, nil)
}

// Close closes a DM or group DM, which comes back when someone writes.
func (c *Client) Close(ctx context.Context, id string) error {
	return c.Call(ctx, "conversations.close", url.Values{"channel": {id}}, nil)
}

// OpenDM opens the DM with one person, or the group DM with several, and
// returns it; it's the one already there if there is one.
func (c *Client) OpenDM(ctx context.Context, users []string) (Conversation, error) {
	var r struct {
		Channel Conversation `json:"channel"`
	}
	err := c.Call(ctx, "conversations.open", url.Values{"users": {strings.Join(users, ",")}, "return_im": {"true"}}, &r)
	return r.Channel, err
}

// SetPref sets one of the user's prefs, as the web client does.
func (c *Client) SetPref(ctx context.Context, name, value string) error {
	return c.Call(ctx, "users.prefs.set", url.Values{"name": {name}, "value": {value}}, nil)
}

// SetCollapsed folds or unfolds a sidebar section. GUESSED: the method
// and the is_collapsed key (the same one Sections reads).
func (c *Client) SetCollapsed(ctx context.Context, section string, collapsed bool) error {
	v := "false"
	if collapsed {
		v = "true"
	}
	return c.Call(ctx, "users.channelSections.set", url.Values{"channel_section_id": {section}, "is_collapsed": {v}}, nil)
}

// MoveConv takes conv out of section from and puts it in to; either may be
// "" (a conversation in no section of its own goes where its kind does).
// GUESSED: users.channelSections.channels.bulkUpdate with insert and remove
// as JSON lists of {channel_section_id, channel_ids}.
func (c *Client) MoveConv(ctx context.Context, conv, from, to string) error {
	list := func(section string) string {
		if section == "" {
			return ""
		}
		b, _ := jsonx.Marshal([]map[string]any{{"channel_section_id": section, "channel_ids": []string{conv}}})
		return string(b)
	}
	f := url.Values{}
	if s := list(to); s != "" {
		f.Set("insert", s)
	}
	if s := list(from); s != "" {
		f.Set("remove", s)
	}
	if len(f) == 0 {
		return nil
	}
	return c.Call(ctx, "users.channelSections.channels.bulkUpdate", f, nil)
}
