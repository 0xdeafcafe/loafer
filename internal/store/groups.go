package store

import (
	"cmp"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/jsonx"
)

// Group is a user group: @handle, which pings whoever is in it.
type Group struct {
	ID     string
	Handle string
	Name   string
}

// Group is who id is, if usergroups.list or a subteam event has said.
func (v View) Group(id string) (Group, bool) {
	g, ok := v.s.folk.groups[id]
	return g, ok
}

// EachGroup calls f with every user group, in no order.
func (v View) EachGroup(f func(Group)) {
	for _, g := range v.s.folk.groups {
		f(g)
	}
}

// ApplyGroups takes usergroups.list.
func (s *Store) ApplyGroups(gs []slack.Group) {
	s.update(func() {
		s.folk.groups = map[string]Group{}
		for _, g := range gs {
			s.putGroup(g)
		}
		s.names++
	})
}

// putGroup files g, or lets it go if it's been deleted. Call with the
// write lock held.
func (s *Store) putGroup(g slack.Group) {
	if g.ID == "" {
		return
	}
	if g.Deleted != 0 {
		delete(s.folk.groups, g.ID)
		return
	}
	old := s.folk.groups[g.ID] // an update may leave out what hasn't changed
	g.Handle, g.Name = cmp.Or(g.Handle, old.Handle), cmp.Or(g.Name, old.Name)
	if g.Handle == "" {
		return
	}
	if s.folk.groups == nil {
		s.folk.groups = map[string]Group{}
	}
	s.folk.groups[g.ID] = Group{g.ID, g.Handle, g.Name}
}

// applySubteam takes subteam_created and subteam_updated, which carry the
// group whole as "subteam".
func (s *Store) applySubteam(ev slack.Event) {
	var e struct {
		Subteam slack.Group `json:"subteam"`
	}
	if jsonx.Unmarshal(ev.Raw, &e) == nil && e.Subteam.ID != "" {
		s.update(func() {
			s.putGroup(e.Subteam)
			s.names++
		})
	}
}
