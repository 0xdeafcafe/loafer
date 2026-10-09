package store

import (
	"cmp"
	"slices"
	"strings"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

// View reads the store. It's only valid inside Read's func; what it
// returns points into the store and must not be changed or kept.
type View struct{ s *Store }

// Read calls f with the store read-locked. Keep f to looking: no I/O, no
// waiting.
func (s *Store) Read(f func(View)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f(View{s})
}

func (v View) Self() string             { return v.s.self }
func (v View) Team() slack.Team         { return v.s.team }
func (v View) Window(id string) *Window { return v.s.windows[id] }

// Conv is the conversation id, or the channel being previewed.
func (v View) Conv(id string) *Conv {
	if c := v.s.convs[id]; c != nil {
		return c
	}
	if p := v.s.preview; p != nil && p.ID == id {
		return p
	}
	return nil
}

// Link is the websocket's state: connecting, live or offline, or "" before
// it's been tried.
func (v View) Link() string { return v.s.link }

// Names goes up whenever people or emoji change.
func (v View) Names() uint64 { return v.s.names }

// Layout goes up whenever Sidebar might say something new: conversations
// come or go, are renamed or reordered, or sections or people change.
func (v View) Layout() uint64 { return v.s.side + v.s.names }

// Person is who id is, or a stand-in carrying the id while users.list
// hasn't said.
func (v View) Person(id string) Person {
	if p := v.s.people[id]; p != nil {
		return *p
	}
	return Person{ID: id, Name: id}
}

// Emoji is a custom emoji's image URL, following aliases; "" if it isn't
// one (it may still be a standard emoji).
func (v View) Emoji(name string) string {
	for range 4 {
		u, ok := v.s.emoji[name]
		if !ok {
			return ""
		}
		alias, isAlias := strings.CutPrefix(u, "alias:")
		if !isAlias {
			return u
		}
		name = alias
	}
	return ""
}

// Title is what a conversation is called: #name, the person for a DM,
// the others for a group DM.
func (v View) Title(c *Conv) string {
	switch c.Kind {
	case IM:
		return v.Person(c.User).Name
	case MPIM:
		self := v.Person(v.s.self).Handle
		parts := strings.Split(c.Name, ", ")
		parts = slices.DeleteFunc(parts, func(p string) bool { return p == self })
		return strings.Join(parts, ", ")
	}
	return c.Name
}

// Sidebar is the sections in order, each with its conversations in the
// order to show them. Slack's sections are used when it gave them; else
// Channels, Direct messages and Apps.
func (v View) Sidebar() []Section {
	placed := make(map[string]bool, len(v.s.convs))
	var out []Section
	add := func(sec Section, ids []string) {
		sec.Convs = nil
		for _, id := range ids {
			if c := v.s.convs[id]; c != nil && !placed[id] {
				placed[id] = true
				sec.Convs = append(sec.Convs, id)
			}
		}
		out = append(out, sec)
	}
	secs := v.s.sections
	if len(secs) == 0 {
		secs = []Section{
			{ID: "channels", Name: "Channels", Type: "channels"},
			{ID: "dms", Name: "Direct messages", Type: "direct_messages"},
			{ID: "apps", Name: "Apps", Type: "recent_apps"},
		}
	}
	// Sections that list their own conversations take them first, so the
	// catch-all sections get only what's left wherever they sit.
	var fill []int
	for _, sec := range secs {
		switch sec.Type {
		case "channels", "direct_messages", "recent_apps":
			fill = append(fill, len(out))
			out = append(out, sec)
		default:
			add(sec, sec.Convs)
		}
	}
	for _, i := range fill {
		sec := out[i]
		var ids []string
		for id, c := range v.s.convs {
			if placed[id] {
				continue
			}
			bot := c.Kind == IM && v.Person(c.User).Bot
			switch sec.Type {
			case "channels":
				if c.Kind == Channel || c.Kind == Private {
					ids = append(ids, id)
				}
			case "direct_messages":
				if (c.Kind == IM && !bot) || c.Kind == MPIM {
					ids = append(ids, id)
				}
			case "recent_apps":
				if bot {
					ids = append(ids, id)
				}
			}
		}
		// ids come from a map, so ties break by id or rows swap between frames
		if sec.Type == "channels" {
			slices.SortFunc(ids, func(a, b string) int {
				return cmp.Or(cmp.Compare(v.s.convs[a].Name, v.s.convs[b].Name), cmp.Compare(a, b))
			})
		} else {
			slices.SortFunc(ids, func(a, b string) int {
				return cmp.Or(cmp.Compare(v.s.convs[b].Latest, v.s.convs[a].Latest), cmp.Compare(a, b))
			})
		}
		for _, id := range ids {
			placed[id] = true
		}
		out[i].Convs = ids
	}
	return slices.DeleteFunc(out, func(s Section) bool { return len(s.Convs) == 0 && s.Type != "standard" })
}
