package store

import "github.com/0xdeafcafe/loafer/internal/slack"

// Kind is what sort of conversation a Conv is.
type Kind uint8

const (
	Channel Kind = iota
	Private
	IM
	MPIM
)

// Conv is a conversation as the sidebar and headers need it.
type Conv struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     Kind   `json:"kind"`
	User     string `json:"user,omitempty"` // the other person, for a DM
	Topic    string `json:"topic,omitempty"`
	LastRead string `json:"last_read,omitempty"`
	Latest   string `json:"latest,omitempty"`
	Mentions int    `json:"mentions,omitempty"`
	Unread   bool   `json:"unread,omitempty"`
	Archived bool   `json:"archived,omitempty"`
	Members  int    `json:"members,omitempty"`
	Muted    bool   `json:"muted,omitempty"`
	Preview  bool   `json:"-"` // a channel you're not in, being read (manage.go)
}

// Person is a member or bot, as messages and lists show them.
type Person struct {
	ID      string `json:"id"`
	Handle  string `json:"handle"`
	Name    string `json:"name"` // display name, else real name, else handle
	Color   string `json:"color,omitempty"`
	Avatar  string `json:"avatar,omitempty"` // 72px image URL
	Bot     bool   `json:"bot,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
}

// Section is a sidebar section, with its conversations in order.
type Section struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Emoji     string   `json:"emoji,omitempty"`
	Type      string   `json:"type"`
	Convs     []string `json:"convs"`
	Collapsed bool     `json:"collapsed,omitempty"`
}

// Window is the recent part of a conversation held in memory, oldest
// first. More says Slack has older messages than these; Newer that it
// has newer ones, after a jump back to an old message (see Around).
type Window struct {
	Msgs  []slack.Message `json:"msgs"`
	More  bool            `json:"more"`
	Newer bool            `json:"newer,omitempty"`
	used  uint64          // the store's clock when last viewed, for eviction
	stale bool            // held across a gap (the socket down, or from the cache): live messages skip it till a Refresh, so none sits after a hole
}

func person(u slack.User) Person {
	p := Person{
		ID: u.ID, Handle: u.Name, Color: u.Color, Avatar: u.Profile.Image72,
		Bot: u.IsBot || u.IsApp, Deleted: u.Deleted,
	}
	for _, n := range []string{u.Profile.DisplayName, u.Profile.RealName, u.RealName, u.Name} {
		if n != "" {
			p.Name = n
			break
		}
	}
	return p
}
