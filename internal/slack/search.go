package slack

import (
	"context"
	"net/url"
	"strconv"
)

// Search is search.messages, the documented method, which takes a session
// token. The web client asks search.modules (module=messages) and
// search.inline instead (docs/slack-webapp-methods.md, Search); their
// args aren't all known, so this stays on the one Slack documents.

// Match is a message search found. Text carries Slack's highlight marks
// (U+E000 before a matched word, U+E001 after) when it was asked for them.
type Match struct {
	Channel struct {
		ID     string `json:"id"`
		Name   string `json:"name"` // a DM's is the other person's id
		IsIM   bool   `json:"is_im"`
		IsMPIM bool   `json:"is_mpim"`
	} `json:"channel"`
	User      string `json:"user"`
	Username  string `json:"username"`
	TS        string `json:"ts"`
	ThreadTS  string `json:"thread_ts"` // not always sent; the permalink has it too
	Text      string `json:"text"`
	Permalink string `json:"permalink"`
}

// Parent is the ts of the thread a reply is in, "" if it isn't one.
func (m Match) Parent() string {
	t := m.ThreadTS
	if t == "" {
		if u, err := url.Parse(m.Permalink); err == nil {
			t = u.Query().Get("thread_ts")
		}
	}
	if t == m.TS {
		return ""
	}
	return t
}

// Found is a page of a search's matches.
type Found struct {
	Matches     []Match
	Page, Pages int
	Total       int
}

// Search finds messages matching query, Slack's modifiers (in:, from:,
// before:, is:thread …) and all, a page of 20 at a time from page 1.
func (c *Client) Search(ctx context.Context, query string, page int) (Found, error) {
	var r struct {
		Messages struct {
			Total   int     `json:"total"`
			Matches []Match `json:"matches"`
			Paging  struct {
				Page  int `json:"page"`
				Pages int `json:"pages"`
			} `json:"paging"`
			Pagination struct {
				Page  int `json:"page"`
				Pages int `json:"page_count"`
			} `json:"pagination"`
		} `json:"messages"`
	}
	err := c.Call(ctx, "search.messages", url.Values{
		"query": {query}, "page": {strconv.Itoa(page)}, "count": {"20"}, "highlight": {"true"},
	}, &r)
	f := Found{Matches: r.Messages.Matches, Page: r.Messages.Paging.Page, Pages: r.Messages.Paging.Pages, Total: r.Messages.Total}
	// paging and pagination say the same; either may be missing.
	if f.Pages == 0 {
		f.Page, f.Pages = r.Messages.Pagination.Page, r.Messages.Pagination.Pages
	}
	if f.Page == 0 {
		f.Page = page
	}
	return f, err
}

// Span is conversations.history between oldest and latest, either of
// which may be "", both ends included: newest first, limit of them.
// Slack fills a page from the latest end, so with only oldest set it's
// the newest messages, not those just after oldest, unless more is false.
func (c *Client) Span(ctx context.Context, channel, oldest, latest string, limit int) (msgs []Message, more bool, err error) {
	f := url.Values{"channel": {channel}, "limit": {strconv.Itoa(limit)}, "inclusive": {"true"}, "include_pin_count": {"false"}}
	if oldest != "" {
		f.Set("oldest", oldest)
	}
	if latest != "" {
		f.Set("latest", latest)
	}
	var r struct {
		Messages []Message `json:"messages"`
		HasMore  bool      `json:"has_more"`
	}
	err = c.Call(ctx, "conversations.history", f, &r)
	return r.Messages, r.HasMore, err
}
