package slacktest

import (
	"cmp"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/jsonx"
)

// The methods for finding, joining and leaving conversations, mute and the
// sidebar's sections, each answering and then telling the sockets as Slack
// would. The section ones speak loafer's guesses (docs/slack-webapp-methods.md).

// manage answers method if it's one of these. Call with the lock held.
func (s *Server) manage(method string, f url.Values) (out map[string]any, code string, ok bool) {
	ch := f.Get("channel")
	c := s.convs[ch]
	switch method {
	case "conversations.list":
		var list []slack.Conversation
		for _, x := range s.convs {
			if x.IsChannel && !x.IsPrivate && !(x.IsArchived && f.Get("exclude_archived") == "true") {
				list = append(list, x.wire())
			}
		}
		slices.SortFunc(list, func(a, b slack.Conversation) int { return strings.Compare(a.Name, b.Name) })
		from, _ := strconv.Atoi(f.Get("cursor"))
		limit, err := strconv.Atoi(f.Get("limit"))
		if err != nil || limit <= 0 {
			limit = 100
		}
		from = min(from, len(list))
		to := min(from+limit, len(list))
		next := ""
		if to < len(list) {
			next = strconv.Itoa(to)
		}
		return map[string]any{"channels": list[from:to], "response_metadata": map[string]string{"next_cursor": next}}, "", true

	case "conversations.join":
		if c == nil || !c.IsChannel {
			return nil, "channel_not_found", true
		}
		if c.IsArchived {
			return nil, "is_archived", true
		}
		c.IsMember = true
		if !slices.Contains(s.order, ch) {
			s.order = append(s.order, ch)
		}
		s.pushAny(map[string]any{"type": "channel_joined", "channel": c.wire()})
		return map[string]any{"channel": c.wire()}, "", true

	case "conversations.leave":
		if c == nil || !slices.Contains(s.order, ch) {
			return nil, "not_in_channel", true
		}
		c.IsMember = false
		s.order = slices.DeleteFunc(s.order, func(id string) bool { return id == ch })
		s.pushAny(map[string]any{"type": "channel_left", "channel": ch})
		return map[string]any{}, "", true

	case "conversations.close":
		if c == nil || !(c.IsIM || c.IsMPIM) {
			return nil, "channel_not_found", true
		}
		c.IsOpen = false
		s.order = slices.DeleteFunc(s.order, func(id string) bool { return id == ch })
		kind := "im_close"
		if c.IsMPIM {
			kind = "mpim_close"
		}
		s.pushAny(map[string]any{"type": kind, "channel": ch})
		return map[string]any{}, "", true

	case "conversations.open":
		users := strings.Split(f.Get("users"), ",")
		if f.Get("users") == "" || len(users) > 8 {
			return nil, "users_list_not_supplied", true
		}
		for _, u := range users {
			if !slices.ContainsFunc(s.users, func(x slack.User) bool { return x.ID == u }) {
				return nil, "user_not_found", true
			}
		}
		for _, x := range s.convs { // the one there already
			if (len(users) == 1 && x.IsIM && x.User == users[0]) || (len(users) > 1 && x.IsMPIM && s.sameGroup(x, users)) {
				x.IsOpen = true
				if !slices.Contains(s.order, x.ID) {
					s.order = append(s.order, x.ID)
				}
				return map[string]any{"channel": x.wire(), "already_open": true}, "", true
			}
		}
		n := &conv{replies: map[string][]slack.Message{}}
		if len(users) == 1 {
			n.Conversation = slack.Conversation{ID: fmt.Sprintf("D0NEW%d", len(s.convs)), IsIM: true, IsOpen: true, User: users[0]}
		} else {
			handles := []string{s.handle(Self)}
			for _, u := range users {
				handles = append(handles, s.handle(u))
			}
			n.Conversation = slack.Conversation{ID: fmt.Sprintf("G0NEW%d", len(s.convs)), Name: "mpdm-" + strings.Join(handles, "--") + "-1",
				IsMPIM: true, IsGroup: true, IsPrivate: true, IsMember: true, IsOpen: true}
		}
		s.convs[n.ID] = n
		s.order = append(s.order, n.ID)
		if n.IsIM {
			s.pushAny(map[string]any{"type": "im_created", "user": n.User, "channel": n.wire()})
		}
		return map[string]any{"channel": n.wire()}, "", true

	case "users.prefs.set":
		if f.Get("name") == "" {
			return nil, "invalid_name", true
		}
		s.prefs[f.Get("name")] = f.Get("value")
		s.pushAny(map[string]any{"type": "pref_change", "name": f.Get("name"), "value": f.Get("value")})
		return map[string]any{}, "", true

	case "users.channelSections.set":
		i := s.section(f.Get("channel_section_id"))
		if i < 0 {
			return nil, "channel_section_not_found", true
		}
		s.sections[i].Collapsed = f.Get("is_collapsed") == "true"
		x := s.sections[i]
		s.pushAny(map[string]any{"type": "channel_section_upserted", "channel_section_id": x.ID, "name": x.Name,
			"emoji": x.Emoji, "channel_section_type": x.Type, "is_collapsed": x.Collapsed})
		return map[string]any{}, "", true

	case "users.channelSections.channels.bulkUpdate":
		type change struct {
			ID    string   `json:"channel_section_id"`
			Convs []string `json:"channel_ids"`
		}
		var insert, remove []change
		if jsonx.Unmarshal([]byte(cmp.Or(f.Get("insert"), "[]")), &insert) != nil || jsonx.Unmarshal([]byte(cmp.Or(f.Get("remove"), "[]")), &remove) != nil {
			return nil, "invalid_arguments", true
		}
		for _, x := range remove {
			if i := s.section(x.ID); i >= 0 {
				s.sections[i].Channels.IDs = slices.DeleteFunc(s.sections[i].Channels.IDs, func(id string) bool { return slices.Contains(x.Convs, id) })
				s.pushAny(map[string]any{"type": "channel_sections_channels_removed", "channel_section_id": x.ID, "channel_ids": x.Convs})
			}
		}
		for _, x := range insert {
			if i := s.section(x.ID); i >= 0 {
				s.sections[i].Channels.IDs = append(s.sections[i].Channels.IDs, x.Convs...)
				s.pushAny(map[string]any{"type": "channel_sections_channels_upserted", "channel_section_id": x.ID, "channel_ids": x.Convs})
			}
		}
		return map[string]any{}, "", true
	}
	return nil, "", false
}

func (s *Server) section(id string) int {
	return slices.IndexFunc(s.sections, func(x slack.Section) bool { return x.ID == id })
}

// sameGroup says whether group DM x is with exactly users (and you).
func (s *Server) sameGroup(x *conv, users []string) bool {
	want := []string{s.handle(Self)}
	for _, u := range users {
		want = append(want, s.handle(u))
	}
	have := strings.Split(strings.TrimSuffix(strings.TrimPrefix(x.Name, "mpdm-"), "-1"), "--")
	slices.Sort(want)
	slices.Sort(have)
	return slices.Equal(want, have)
}

func (s *Server) handle(id string) string {
	if i := slices.IndexFunc(s.users, func(x slack.User) bool { return x.ID == id }); i >= 0 {
		return s.users[i].Name
	}
	return id
}

// Section is a section of the sidebar as the server holds it, for tests.
func (s *Server) Section(id string) (slack.Section, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.section(id); i >= 0 {
		return s.sections[i], true
	}
	return slack.Section{}, false
}

// Pref is a pref as the server holds it, for tests.
func (s *Server) Pref(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prefs[name]
}
