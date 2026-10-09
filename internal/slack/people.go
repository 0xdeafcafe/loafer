package slack

import (
	"context"
	"maps"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/0xdeafcafe/photon/jsonx"
)

// What the people lane asks Slack: presence over the websocket, a person's
// profile, user groups and a DM with someone. Shapes are the public API's
// and the web client's as far as docs/slack-webapp-methods.md knows them.

// presenceDelay is how long Watch waits for more ids, as the web client does.
const presenceDelay = 100 * time.Millisecond

// presence is who the socket is asked about. Subscriptions belong to one
// socket, so a new one is told them all again.
type presence struct {
	mu    sync.Mutex
	want  map[string]bool // everyone ever watched
	fresh []string        // watched, not yet sent on this socket
	out   chan []byte     // frames for Listen to write; nil while there's no socket
	timer *time.Timer     // armed only while fresh ids wait
}

// Watch asks for presence_change events about ids, sent in one frame
// 100ms after the last call. It never waits, and does nothing for ids
// already asked about.
func (c *Client) Watch(ids []string) {
	p := &c.pres
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.want == nil {
		p.want = map[string]bool{}
	}
	for _, id := range ids {
		if id != "" && !p.want[id] {
			p.want[id] = true
			p.fresh = append(p.fresh, id)
		}
	}
	p.arm()
}

// arm starts the wait for a frame, if there's one to send and a socket
// to send it on. Call with the lock held.
func (p *presence) arm() {
	if p.out != nil && len(p.fresh) > 0 && p.timer == nil {
		p.timer = time.AfterFunc(presenceDelay, p.flush)
	}
}

func (p *presence) flush() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.timer = nil
	if p.out == nil || len(p.fresh) == 0 {
		return
	}
	b, err := jsonx.Marshal(struct {
		Type string   `json:"type"`
		IDs  []string `json:"ids"`
	}{"presence_sub", p.fresh})
	if err != nil {
		return
	}
	select {
	case p.out <- b:
		p.fresh = nil
	default: // the socket isn't taking frames; the next Watch tries again
	}
}

// up is a socket opening: everyone wanted is fresh again. It returns the
// frames to write to it.
func (p *presence) up() <-chan []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.out = make(chan []byte, 16)
	p.fresh = slices.Sorted(maps.Keys(p.want))
	p.arm()
	return p.out
}

func (p *presence) down() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.out = nil
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
}

// Group is a user group, as usergroups.list and subteam_* events carry it.
type Group struct {
	ID      string `json:"id"`
	Handle  string `json:"handle"`
	Name    string `json:"name"`
	Count   int    `json:"user_count"`
	Deleted int64  `json:"date_delete"` // when it went, 0 while it's there
}

// UserGroups lists the workspace's user groups.
func (c *Client) UserGroups(ctx context.Context) ([]Group, error) {
	var r struct {
		Groups []Group `json:"usergroups"`
	}
	err := c.Call(ctx, "usergroups.list", url.Values{"include_count": {"true"}}, &r)
	return r.Groups, err
}

// UserInfo is one person's profile.
func (c *Client) UserInfo(ctx context.Context, id string) (User, error) {
	var r struct {
		User User `json:"user"`
	}
	err := c.Call(ctx, "users.info", url.Values{"user": {id}}, &r)
	return r.User, err
}

// OpenDM opens (or finds) your DM with user. return_im asks for the
// conversation whole rather than just its id.
func (c *Client) OpenDM(ctx context.Context, user string) (Conversation, error) {
	var r struct {
		Channel Conversation `json:"channel"`
	}
	err := c.Call(ctx, "conversations.open", url.Values{"users": {user}, "return_im": {"true"}}, &r)
	return r.Channel, err
}
