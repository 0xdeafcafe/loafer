package store

import (
	"cmp"
	"encoding/json/jsontext"
	"slices"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/jsonx"
)

// Modals an app opens (docs/slack-webapp-methods.md §6): pressing a button
// sends a client_token, the app opens a view, and Slack pushes view_opened
// carrying the same token. Only views asked for here are taken, as the web
// client does, so a modal opened on another device stays there.

// Modal is one view on the stack, as Slack sent it.
type Modal struct {
	ID, Hash, AppID string
	View            jsontext.Value    // title, blocks, submit, close, state…
	Errors          map[string]string // by block_id, from the last submit
}

type modals struct {
	stack  []*Modal // the top one shows
	expect []string // client_tokens sent lately, newest last
}

const keepTokens = 8

// ExpectView says a view may come for token.
func (s *Store) ExpectView(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.md.expect = append(s.md.expect, token)
	s.md.expect = s.md.expect[max(0, len(s.md.expect)-keepTokens):]
}

// applyView takes view_opened, view_pushed, view_updated and view_closed.
// UNCERTAIN: only view_opened and view_updated were seen in the web
// client; the other two are by analogy, and view may be an object or a
// string of JSON.
func (s *Store) applyView(ev slack.Event) {
	var e struct {
		View     jsontext.Value `json:"view"`
		ViewID   string         `json:"view_id"`
		ViewType string         `json:"view_type"`
		Previous string         `json:"previous_view_id"`
		Token    string         `json:"client_token"`
		AppID    string         `json:"app_id"`
	}
	if jsonx.Unmarshal(ev.Raw, &e) != nil {
		return
	}
	if len(e.View) > 0 && e.View[0] == '"' {
		var raw string
		if jsonx.Unmarshal(e.View, &raw) != nil {
			return
		}
		e.View = jsontext.Value(raw)
	}
	var v struct {
		ID    string `json:"id"`
		Hash  string `json:"hash"`
		Type  string `json:"type"`
		AppID string `json:"app_id"`
	}
	if len(e.View) > 0 && jsonx.Unmarshal(e.View, &v) != nil {
		return
	}
	m := &Modal{ID: cmp.Or(e.ViewID, v.ID), Hash: v.Hash, AppID: cmp.Or(e.AppID, v.AppID), View: e.View}
	if m.ID == "" {
		return
	}
	s.update(func() {
		at := s.md.at(m.ID)
		switch ev.Type {
		case "view_closed":
			if at >= 0 {
				s.md.stack = s.md.stack[:at]
			}
		case "view_updated":
			if at >= 0 && len(m.View) > 0 {
				s.md.stack[at] = m
			}
		case "view_opened", "view_pushed":
			if cmp.Or(e.ViewType, v.Type) != "modal" || len(m.View) == 0 {
				return // a home tab, say
			}
			on := s.md.at(e.Previous)
			if i := slices.Index(s.md.expect, e.Token); i >= 0 && e.Token != "" {
				s.md.expect = slices.Delete(s.md.expect, i, i+1)
			} else if on < 0 {
				return // not asked for here
			}
			s.md.stack = append(s.md.stack[:on+1], m)
		}
	})
}

// at is where the view id is on the stack, or -1.
func (md *modals) at(id string) int {
	if id == "" {
		return -1
	}
	return slices.IndexFunc(md.stack, func(m *Modal) bool { return m.ID == id })
}

// CloseModal lets go of the view id, and any pushed on it.
func (s *Store) CloseModal(id string) {
	s.update(func() {
		if at := s.md.at(id); at >= 0 {
			s.md.stack = s.md.stack[:at]
		}
	})
}

// ModalErrors keeps what a submit said was wrong with view id.
func (s *Store) ModalErrors(id string, errs map[string]string) {
	s.update(func() {
		if at := s.md.at(id); at >= 0 {
			m := *s.md.stack[at]
			m.Errors = errs
			s.md.stack[at] = &m
		}
	})
}

// Modal is the view showing, or nil.
func (v View) Modal() *Modal {
	if n := len(v.s.md.stack); n > 0 {
		return v.s.md.stack[n-1]
	}
	return nil
}
