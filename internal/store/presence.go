package store

import (
	"cmp"
	"context"
	"errors"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/jsonx"
)

// Who's about, and how they've changed. Presence is kept apart from
// Person and doesn't touch names, so a flip between active and away wakes
// the UI without drawing a single message again; only what a drawn
// message shows (name, avatar, status) does that.

// folk is what people have besides their profiles.
type folk struct {
	presence map[string]string // "active" or "away", for those watched
	groups   map[string]Group  // user groups by id (groups.go)
}

// Presence is "active" or "away", or "" until Slack has said.
func (v View) Presence(id string) string { return v.s.folk.presence[id] }

// applyPresence takes presence_change, for one user or a batch of them,
// and manual_presence_change, which is you.
func (s *Store) applyPresence(ev slack.Event) {
	var e struct {
		User     string   `json:"user"`
		Users    []string `json:"users"` // the batch form
		Presence string   `json:"presence"`
	}
	if jsonx.Unmarshal(ev.Raw, &e) != nil || (e.Presence != "active" && e.Presence != "away") {
		return
	}
	changed := false
	s.mu.Lock()
	ids := e.Users
	switch {
	case ev.Type == "manual_presence_change":
		ids = []string{s.self}
	case e.User != "":
		ids = append(ids, e.User)
	}
	for _, id := range ids {
		if id == "" || s.folk.presence[id] == e.Presence {
			continue
		}
		if s.folk.presence == nil {
			s.folk.presence = map[string]string{}
		}
		s.folk.presence[id] = e.Presence
		changed = true
	}
	s.mu.Unlock()
	if changed {
		s.version.Add(1) // not bump: presence is nothing to save
		select {
		case s.changed <- struct{}{}:
		default:
		}
	}
}

// ApplyUser takes one person from user_change, user_status_changed or
// users.info. A partial one (a status alone, as user_status_changed may
// bring) changes only the status. Names go up only when something a
// drawn message shows has changed.
func (s *Store) ApplyUser(u slack.User) {
	if u.ID == "" {
		return
	}
	s.update(func() {
		old, p := s.people[u.ID], person(u)
		if old != nil && u.Name == "" && p.Name == "" {
			keep := *old
			keep.StatusEmoji, keep.StatusText, keep.StatusUntil = p.StatusEmoji, p.StatusText, p.StatusUntil
			p = keep
		}
		if old == nil || old.Name != p.Name || old.Avatar != p.Avatar || old.Bot != p.Bot || old.Deleted != p.Deleted ||
			old.StatusEmoji != p.StatusEmoji || old.StatusUntil != p.StatusUntil {
			s.names++
		}
		s.people[p.ID] = &p
	})
}

// PersonInfo fetches id's profile, which may hold more than users.list
// did (or be someone it never listed).
func (s *Store) PersonInfo(ctx context.Context, c *slack.Client, id string) error {
	u, err := c.UserInfo(ctx, id)
	if err == nil {
		s.ApplyUser(u)
	}
	return err
}

// DM is your DM with user: the one the sidebar has, else opened through
// conversations.open and kept.
func (s *Store) DM(ctx context.Context, c *slack.Client, user string) (string, error) {
	conv := ""
	s.Read(func(v View) {
		for id, x := range v.s.convs {
			if x.Kind == IM && x.User == user {
				conv = id
			}
		}
	})
	if conv != "" {
		return conv, nil
	}
	x, err := c.OpenDM(ctx, user)
	if err != nil || x.ID == "" {
		return "", cmp.Or(err, errors.New("conversations.open: no conversation"))
	}
	x.IsIM, x.User = true, user
	s.update(func() { s.putConv(x) })
	return x.ID, nil
}
