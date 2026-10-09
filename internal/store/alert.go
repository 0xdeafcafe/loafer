package store

import (
	"encoding/json/jsontext"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xdeafcafe/loafer/internal/notify"
	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/jsonx"
)

// TypingFor is how long after its last user_typing someone counts as
// still typing.
const TypingFor = 5 * time.Second

// alertState is what notifications and typing indicators need. prefs is
// guarded by the store's lock; the rest has its own.
type alertState struct {
	prefs notify.Prefs
	notes chan notify.Note
	watch atomic.Value // watching

	tmu    sync.Mutex
	typing map[string]map[string]time.Time // conversation -> who -> when it lapses
}

// watching is what the UI is showing.
type watching struct {
	conv    string
	focused bool // the terminal has focus
}

func newAlertState() alertState {
	return alertState{notes: make(chan notify.Note, 16), typing: map[string]map[string]time.Time{}}
}

// Notes brings the messages that should notify you, as they land. It
// holds a few; a burst past that is dropped, not waited on.
func (s *Store) Notes() <-chan notify.Note { return s.al.notes }

// Watch says which conversation is open and whether the terminal has
// focus, so that it doesn't notify about what you're looking at and
// typing in other conversations doesn't wake the UI.
func (s *Store) Watch(conv string, focused bool) { s.al.watch.Store(watching{conv, focused}) }

func (s *Store) watching() watching {
	w, _ := s.al.watch.Load().(watching)
	return w
}

// applyAlert takes the websocket events that notifications and typing
// indicators are made of.
func (s *Store) applyAlert(ev slack.Event) {
	switch ev.Type {
	case "pref_change":
		var e struct {
			Name  string         `json:"name"`
			Value jsontext.Value `json:"value"`
		}
		if jsonx.Unmarshal(ev.Raw, &e) == nil {
			s.update(func() { s.al.prefs.Set(e.Name, e.Value); s.syncMuted() })
		}

	case "dnd_updated":
		// The shape is a guess: the user, and their dnd_status as client.userBoot has dnd.
		var e struct {
			User   string         `json:"user"`
			Status jsontext.Value `json:"dnd_status"`
		}
		if jsonx.Unmarshal(ev.Raw, &e) == nil {
			s.update(func() {
				if e.User == "" || e.User == s.self {
					s.al.prefs.SetDND(e.Status)
				}
			})
		}

	case "user_typing":
		var e struct {
			Channel  string `json:"channel"`
			User     string `json:"user"`
			ThreadTS string `json:"thread_ts"`
		}
		if jsonx.Unmarshal(ev.Raw, &e) != nil || e.Channel == "" || e.User == "" || e.ThreadTS != "" {
			return // typing in a thread is the thread's
		}
		now := time.Now()
		s.al.tmu.Lock()
		for conv, who := range s.al.typing {
			for u, until := range who {
				if now.After(until) {
					delete(who, u)
				}
			}
			if len(who) == 0 {
				delete(s.al.typing, conv)
			}
		}
		if s.al.typing[e.Channel] == nil {
			s.al.typing[e.Channel] = map[string]time.Time{}
		}
		s.al.typing[e.Channel][e.User] = now.Add(TypingFor)
		s.al.tmu.Unlock()
		if s.watching().conv == e.Channel {
			select { // not bump: nothing but the typing line has changed
			case s.changed <- struct{}{}:
			default:
			}
		}
	}
}

// Typing is who is typing in conv at now, other than you, and when the
// first of them lapses (zero if none are).
func (v View) Typing(conv string, now time.Time) (ids []string, until time.Time) {
	v.s.al.tmu.Lock()
	defer v.s.al.tmu.Unlock()
	for u, t := range v.s.al.typing[conv] {
		if u != v.s.self && now.Before(t) {
			ids = append(ids, u)
			if until.IsZero() || t.Before(until) {
				until = t
			}
		}
	}
	slices.Sort(ids)
	return ids, until
}

// alert decides whether m, just added to conv, should notify you, and if
// it should queues it for the UI.
func (s *Store) alert(conv string, m slack.Message) {
	s.al.tmu.Lock()
	delete(s.al.typing[conv], m.User) // it's been sent, so they've stopped
	s.al.tmu.Unlock()

	s.mu.RLock()
	c := s.convs[conv]
	if c == nil {
		s.mu.RUnlock()
		return
	}
	v := View{s}
	w := s.watching()
	in := notify.Input{
		Conv: conv, Self: s.self, User: m.User, Subtype: m.Subtype, Text: m.Text,
		Direct:  c.Kind == IM || c.Kind == MPIM,
		Looking: w.focused && w.conv == conv,
	}
	if m.ThreadTS != "" && m.ThreadTS != m.TS {
		in.Reply = true
		// shortcut: the parent has to be held to know it's yours; a thread
		// followed by subscription alone (thread_subscribed) isn't known.
		if win, i := s.held(conv, m.ThreadTS); i >= 0 {
			p := win.Msgs[i]
			in.Following = p.User == s.self || slices.Contains(p.ReplyUsers, s.self)
		}
	}
	if !s.al.prefs.Wants(in, time.Now()) {
		s.mu.RUnlock()
		return
	}
	body := notify.Plain(m.Text, func(id string) string { return v.Person(id).Name })
	if body == "" {
		body = "sent an attachment"
	}
	if c.Kind != IM {
		who := m.Username
		switch {
		case m.User != "":
			who = v.Person(m.User).Name
		case m.BotProfile != nil:
			who = m.BotProfile.Name
		}
		if who != "" {
			body = who + ": " + body
		}
	}
	title := v.Title(c)
	if c.Kind == Channel || c.Kind == Private {
		title = "#" + title
	}
	n := notify.Note{Team: s.team.ID, Conv: conv, TS: m.TS, Title: title, Body: body}
	if in.Reply {
		n.Thread = m.ThreadTS
	}
	s.mu.RUnlock()
	select {
	case s.al.notes <- n:
	default:
	}
}
