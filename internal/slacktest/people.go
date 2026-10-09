package slacktest

import (
	"net/url"
	"slices"

	"github.com/coder/websocket"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/jsonx"
)

// People: presence, profiles, user groups and DMs on request.

// Groups.
const Bakers = "S0BAKERS"

// folk is what the people methods keep. Guarded by Server.mu.
type folk struct {
	presence map[string]string // "active" or "away"; those missing never get an event
	subs     []string          // ids asked about over presence_sub, in order
	groups   []slack.Group
}

func (s *Server) seedPeople() {
	s.folk.presence = map[string]string{Priya: "active", Jo: "away", Tomas: "active"}
	s.folk.groups = []slack.Group{{ID: Bakers, Handle: "bakers", Name: "Bakers", Count: 3}}
	for i := range s.users {
		if s.users[i].ID == Priya {
			p := &s.users[i].Profile
			p.Title, p.Email, p.Pronouns = "Staff engineer", "priya@crumb.example", "she/her"
			p.StatusEmoji, p.StatusText = ":palm_tree:", "on holiday"
			s.users[i].TZ = "Asia/Kolkata"
		}
	}
}

// Presence says user is now presence, "active" or "away", to every socket.
func (s *Server) Presence(user, presence string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.folk.presence[user] = presence
	s.pushAny(map[string]any{"type": "presence_change", "user": user, "presence": presence})
}

// Subscribed is every id the sockets have asked presence for, in order.
func (s *Server) Subscribed() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.folk.subs)
}

// frame takes a frame a socket sent that isn't a ping: presence_sub is
// answered, to that socket only, with what it asked for as batches.
func (s *Server) frame(c *websocket.Conn, b []byte) {
	var f struct {
		Type string   `json:"type"`
		IDs  []string `json:"ids"`
	}
	if jsonx.Unmarshal(b, &f) != nil || f.Type != "presence_sub" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.folk.subs = append(s.folk.subs, f.IDs...)
	by := map[string][]string{}
	for _, id := range f.IDs {
		if p := s.folk.presence[id]; p != "" {
			by[p] = append(by[p], id)
		}
	}
	for _, p := range []string{"active", "away"} {
		if len(by[p]) == 0 {
			continue
		}
		ev, _ := jsonx.Marshal(map[string]any{"type": "presence_change", "users": by[p], "presence": p})
		write(c, ev)
	}
}

// serveFolk answers the people methods; ok says whether it was one. Call
// with the lock held.
func (s *Server) serveFolk(method string, f url.Values) (out map[string]any, code string, ok bool) {
	switch method {
	case "usergroups.list":
		return map[string]any{"usergroups": s.folk.groups}, "", true

	case "users.info":
		for _, u := range s.users {
			if u.ID == f.Get("user") {
				return map[string]any{"user": u}, "", true
			}
		}
		return nil, "user_not_found", true
	}
	return nil, "", false
}
