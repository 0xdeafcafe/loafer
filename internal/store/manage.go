package store

import (
	"slices"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/jsonx"
)

// What managing the sidebar changes: joining, leaving and closing, mute,
// and the sections' fold and membership. Each applies at once and returns
// what the caller needs to send it to Slack, or to put it back if Slack
// says no. The websocket's events say the same things again, so all of it
// is idempotent.

// catchAll says whether a section type takes whatever isn't in a section of
// its own, rather than listing conversations itself.
func catchAll(t string) bool {
	return t == "channels" || t == "direct_messages" || t == "recent_apps"
}

// syncMuted copies the mute prefs onto the conversations. Call with the
// write lock held.
func (s *Store) syncMuted() {
	for id, c := range s.convs {
		c.Muted = s.al.prefs.Muted(id)
	}
}

// Put takes a conversation you've joined or opened, as Slack describes it.
func (s *Store) Put(c slack.Conversation) {
	if !c.IsIM && !c.IsMPIM {
		c.IsMember = true
	}
	s.update(func() { s.putConv(c) })
}

// Preview holds a channel you're not in, to be read before joining. There's
// one at a time; it isn't in the sidebar, the cache or anything counted.
func (s *Store) Preview(c slack.Conversation) {
	about := c.Topic.Value
	if about == "" {
		about = c.Purpose.Value
	}
	s.update(func() {
		s.preview = &Conv{ID: c.ID, Name: c.Name, Kind: Channel, Topic: about, Members: c.NumMembers, Preview: true}
	})
}

// Drop lets go of a conversation you've left or closed, and gives it back
// so it can be put back.
func (s *Store) Drop(id string) (c Conv, ok bool) {
	s.update(func() {
		if v := s.convs[id]; v != nil {
			c, ok = *v, true
			delete(s.convs, id)
			s.side++
		}
	})
	return c, ok
}

// Restore puts back what Drop gave.
func (s *Store) Restore(c Conv) { s.update(func() { s.convs[c.ID] = &c; s.side++ }) }

// Mute mutes or unmutes id, and gives the value for the muted_channels pref.
func (s *Store) Mute(id string, on bool) (value string) {
	s.update(func() {
		value = s.al.prefs.Mute(id, on)
		s.syncMuted()
	})
	return value
}

// Collapse folds or unfolds a section, saying whether it's one held.
func (s *Store) Collapse(section string, on bool) (ok bool) {
	s.update(func() {
		for i := range s.sections {
			if s.sections[i].ID == section {
				s.sections[i].Collapsed, ok = on, true
				s.side++
			}
		}
	})
	return ok
}

// Move puts conv in section to, taking it out of the one it was in ("" for
// none, which is where it goes by its kind). It gives the section it left,
// and false if to isn't one held.
func (s *Store) Move(conv, to string) (from string, ok bool) {
	s.update(func() {
		if from, ok = s.place(conv, to); ok {
			s.side++
		}
	})
	return from, ok
}

// place is Move with the lock held.
func (s *Store) place(conv, to string) (from string, ok bool) {
	if to != "" && !slices.ContainsFunc(s.sections, func(x Section) bool { return x.ID == to }) {
		return "", false // nothing moves
	}
	ok = true
	for i := range s.sections {
		sec := &s.sections[i]
		if j := slices.Index(sec.Convs, conv); j >= 0 {
			from = sec.ID
			sec.Convs = slices.Delete(slices.Clone(sec.Convs), j, j+1)
		}
		if sec.ID == to && !catchAll(sec.Type) {
			sec.Convs = append(slices.Clone(sec.Convs), conv)
		}
	}
	return from, ok
}

// Sections is every section as held, empty ones too.
func (v View) Sections() []Section { return slices.Clone(v.s.sections) }

// SectionOf is the section that lists conv itself, "" if it's in the one
// for its kind.
func (v View) SectionOf(conv string) string {
	for _, sec := range v.s.sections {
		if slices.Contains(sec.Convs, conv) {
			return sec.ID
		}
	}
	return ""
}

// applySection takes the websocket's section events. The bodies are
// guesses from the web client's handlers (docs/slack-webapp-methods.md),
// so each field is optional.
func (s *Store) applySection(ev slack.Event) {
	var e struct {
		ID    string   `json:"channel_section_id"`
		Name  string   `json:"name"`
		Emoji string   `json:"emoji"`
		Type  string   `json:"channel_section_type"`
		Next  string   `json:"next_channel_section_id"`
		Fold  *bool    `json:"is_collapsed"`
		Convs []string `json:"channel_ids"`
		Page  *struct {
			IDs []string `json:"channel_ids"`
		} `json:"channel_ids_page"`
	}
	if jsonx.Unmarshal(ev.Raw, &e) != nil || e.ID == "" {
		return
	}
	s.update(func() {
		i := slices.IndexFunc(s.sections, func(x Section) bool { return x.ID == e.ID })
		switch ev.Type {
		case "channel_section_upserted":
			if i < 0 {
				at := slices.IndexFunc(s.sections, func(x Section) bool { return e.Next != "" && x.ID == e.Next })
				if at < 0 {
					at = len(s.sections)
				}
				s.sections = slices.Insert(s.sections, at, Section{ID: e.ID})
				i = at
			}
			sec := &s.sections[i]
			if e.Name != "" {
				sec.Name, sec.Emoji = e.Name, e.Emoji
			}
			if e.Type != "" {
				sec.Type = e.Type
			}
			if e.Fold != nil {
				sec.Collapsed = *e.Fold
			}
			if e.Page != nil {
				sec.Convs = e.Page.IDs
			}
		case "channel_section_deleted":
			if i >= 0 {
				s.sections = slices.Delete(s.sections, i, i+1)
			}
		case "channel_sections_channels_upserted":
			for _, id := range e.Convs {
				s.place(id, e.ID)
			}
		case "channel_sections_channels_removed":
			if i >= 0 {
				s.sections[i].Convs = slices.DeleteFunc(slices.Clone(s.sections[i].Convs), func(id string) bool { return slices.Contains(e.Convs, id) })
			}
		}
		s.side++ // any of these may reshape the sidebar
	})
}
