package slack

import (
	"context"
	"encoding/json/jsontext"
	"net/url"
	"strconv"
)

// The calls a client makes as it starts. Response shapes are Slack's
// (from its web client, see docs/slack-webapp-methods.md); fields we
// don't use aren't decoded. Anything whose shape varies stays raw.

// Text is Slack's {"value": ...} wrapper for topics and purposes.
type Text struct {
	Value string `json:"value"`
}

// Conversation is a channel, private channel, DM or group DM as boot and
// conversations.* describe it.
type Conversation struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	IsChannel  bool           `json:"is_channel"`
	IsGroup    bool           `json:"is_group"`
	IsIM       bool           `json:"is_im"`
	IsMPIM     bool           `json:"is_mpim"`
	IsPrivate  bool           `json:"is_private"`
	IsArchived bool           `json:"is_archived"`
	IsMember   bool           `json:"is_member"`
	IsOpen     bool           `json:"is_open"`
	User       string         `json:"user"` // the other person, for a DM
	Topic      Text           `json:"topic"`
	Purpose    Text           `json:"purpose"`
	LastRead   string         `json:"last_read"`
	Latest     jsontext.Value `json:"latest"` // a ts, or a whole message, by endpoint
	NumMembers int            `json:"num_members"`
	Updated    int64          `json:"updated"`
}

// UserBoot is client.userBoot: who you are, and the conversations you're in.
type UserBoot struct {
	Self     User           `json:"self"`
	Team     Team           `json:"team"`
	Channels []Conversation `json:"channels"`
	IMs      []Conversation `json:"ims"`
	Prefs    jsontext.Value `json:"prefs"` // kept raw until notifications read it
	DND      jsontext.Value `json:"dnd"`
}

type Team struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Domain string `json:"domain"`
}

func (c *Client) UserBoot(ctx context.Context) (UserBoot, error) {
	var r UserBoot
	err := c.Call(ctx, "client.userBoot", url.Values{
		"_x_reason":                      {"initial-data"},
		"version_all_channels":           {"false"},
		"omit_channels":                  {"false"},
		"include_min_version_bump_check": {"1"},
	}, &r)
	return r, err
}

// Snapshot is one conversation's read state in client.counts.
type Snapshot struct {
	ID           string `json:"id"`
	LastRead     string `json:"last_read"`
	Latest       string `json:"latest"`
	MentionCount int    `json:"mention_count"`
	HasUnreads   bool   `json:"has_unreads"`
}

// Counts is client.counts: unread and mention state for everything.
type Counts struct {
	Channels []Snapshot `json:"channels"`
	MPIMs    []Snapshot `json:"mpims"`
	IMs      []Snapshot `json:"ims"`
	Threads  struct {
		HasUnreads   bool `json:"has_unreads"`
		MentionCount int  `json:"mention_count"`
	} `json:"threads"`
	Saved struct {
		Uncompleted        int `json:"uncompleted_count"`
		UncompletedOverdue int `json:"uncompleted_overdue_count"`
	} `json:"saved"`
	Activity jsontext.Value `json:"activity_v2"` // see Badge
}

func (c *Client) Counts(ctx context.Context) (Counts, error) {
	var r Counts
	err := c.Call(ctx, "client.counts", url.Values{
		"thread_counts_by_channel": {"true"},
		"org_wide_aware":           {"true"},
		"include_file_channels":    {"true"},
	}, &r)
	return r, err
}

// User is a member of the workspace.
type User struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	RealName string `json:"real_name"`
	Deleted  bool   `json:"deleted"`
	IsBot    bool   `json:"is_bot"`
	IsApp    bool   `json:"is_app_user"`
	Color    string `json:"color"`
	TZ       string `json:"tz"`
	Profile  struct {
		DisplayName string `json:"display_name"`
		RealName    string `json:"real_name"`
		Image48     string `json:"image_48"`
		Image72     string `json:"image_72"`
		StatusEmoji string `json:"status_emoji"`
		StatusText  string `json:"status_text"`
		BotID       string `json:"bot_id"`
	} `json:"profile"`
}

// Users lists everyone, a page at a time, calling page with each.
func (c *Client) Users(ctx context.Context, page func([]User)) error {
	cursor := ""
	for {
		var r struct {
			Members []User `json:"members"`
			Meta    struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		f := url.Values{"limit": {"500"}}
		if cursor != "" {
			f.Set("cursor", cursor)
		}
		if err := c.Call(ctx, "users.list", f, &r); err != nil {
			return err
		}
		page(r.Members)
		if cursor = r.Meta.NextCursor; cursor == "" {
			return nil
		}
	}
}

// Emoji is the workspace's custom emoji: name to image URL, or "alias:other".
func (c *Client) Emoji(ctx context.Context) (map[string]string, error) {
	var r struct {
		Emoji map[string]string `json:"emoji"`
	}
	err := c.Call(ctx, "emoji.list", nil, &r)
	return r.Emoji, err
}

// Section is a sidebar section. The shape is a best reading of the web
// client; if Slack says otherwise, the sidebar falls back to its default
// grouping.
type Section struct {
	ID        string `json:"channel_section_id"`
	Name      string `json:"name"`
	Type      string `json:"type"` // standard, stars, direct_messages, recent_apps, channels, ...
	Emoji     string `json:"emoji"`
	Next      string `json:"next_channel_section_id"`
	Collapsed bool   `json:"is_collapsed"` // ponytail: guessed key, check against a trace
	Channels  struct {
		IDs    []string `json:"channel_ids"`
		Cursor string   `json:"cursor"`
	} `json:"channel_ids_page"`
}

func (c *Client) Sections(ctx context.Context) ([]Section, error) {
	var r struct {
		Sections []Section `json:"channel_sections"`
	}
	err := c.Call(ctx, "users.channelSections.list", nil, &r)
	return r.Sections, err
}

// Message is one message as conversations.history and the websocket give
// it. Blocks, attachments and files stay raw for the renderer.
type Message struct {
	Type        string         `json:"type"`
	Subtype     string         `json:"subtype"`
	TS          string         `json:"ts"`
	User        string         `json:"user"`
	BotID       string         `json:"bot_id"`
	Username    string         `json:"username"` // a bot's chosen name
	Text        string         `json:"text"`
	ThreadTS    string         `json:"thread_ts"`
	ReplyCount  int            `json:"reply_count"`
	ReplyUsers  []string       `json:"reply_users"`
	LatestReply string         `json:"latest_reply"`
	Edited      *struct{}      `json:"edited"`
	PinnedTo    []string       `json:"pinned_to"` // the conversations it's pinned in
	Reactions   []Reaction     `json:"reactions"`
	Blocks      jsontext.Value `json:"blocks"`
	Attachments jsontext.Value `json:"attachments"`
	Files       jsontext.Value `json:"files"`
	BotProfile  *struct {
		Name  string `json:"name"`
		Icons struct {
			Image48 string `json:"image_48"`
			Image72 string `json:"image_72"`
		} `json:"icons"`
	} `json:"bot_profile"`
}

type Reaction struct {
	Name  string   `json:"name"`
	Count int      `json:"count"`
	Users []string `json:"users"`
}

// History is a page of a conversation, newest first as Slack sends it,
// older than before ("" for the latest).
func (c *Client) History(ctx context.Context, channel, before string, limit int) (msgs []Message, more bool, err error) {
	f := url.Values{"channel": {channel}, "limit": {strconv.Itoa(limit)}, "include_pin_count": {"false"}}
	if before != "" {
		f.Set("latest", before)
	}
	var r struct {
		Messages []Message `json:"messages"`
		HasMore  bool      `json:"has_more"`
	}
	err = c.Call(ctx, "conversations.history", f, &r)
	return r.Messages, r.HasMore, err
}

// Post sends text to channel (a thread reply when threadTS is set) and
// returns the message as Slack stored it.
func (c *Client) Post(ctx context.Context, channel, text, threadTS string) (Message, error) {
	f := url.Values{"channel": {channel}, "text": {text}, "link_names": {"true"}}
	if threadTS != "" {
		f.Set("thread_ts", threadTS)
	}
	var r struct {
		Message Message `json:"message"`
	}
	err := c.Call(ctx, "chat.postMessage", f, &r)
	return r.Message, err
}

// Update replaces the text of the message at ts.
func (c *Client) Update(ctx context.Context, channel, ts, text string) error {
	return c.Call(ctx, "chat.update", url.Values{"channel": {channel}, "ts": {ts}, "text": {text}, "link_names": {"true"}}, nil)
}

// Delete removes the message at ts.
func (c *Client) Delete(ctx context.Context, channel, ts string) error {
	return c.Call(ctx, "chat.delete", url.Values{"channel": {channel}, "ts": {ts}}, nil)
}

// Mark moves channel's read marker to ts.
func (c *Client) Mark(ctx context.Context, channel, ts string) error {
	return c.Call(ctx, "conversations.mark", url.Values{"channel": {channel}, "ts": {ts}}, nil)
}
