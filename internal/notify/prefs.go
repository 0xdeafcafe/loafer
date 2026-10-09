// Package notify is Slack's notification rules and how a notification
// reaches you. It knows nothing of the UI or the store, so the TUI and
// notifyd (docs/design.md) both use it: they hold a Prefs, ask it Wants
// for each new message, and deliver what it says yes to.
package notify

import (
	"encoding/json/jsontext"
	"strings"
	"time"
	"unicode"

	"github.com/0xdeafcafe/photon/jsonx"
)

// How much of a conversation notifies, which Slack lets you set for
// everything and again for each conversation.
const (
	levelAll      = "all"
	levelMentions = "mentions"
	levelNone     = "none"
)

// Prefs is what decides whether a message notifies: the notification
// settings from client.userBoot's prefs and dnd, kept up by pref_change
// and dnd_updated. The key names are from Slack's web client and several
// are guesses (marked below), so a wrong one means a missing setting,
// never a failure. Not safe for concurrent use; the holder locks.
type Prefs struct {
	level    string   // everything, mentions or nothing, for conversations without their own
	muted    set      // prefs.muted_channels
	loud     set      // prefs.loud_channels, a guess: conversations on everything
	never    set      // prefs.never_channels, a guess: conversations on nothing
	noAt     set      // prefs.at_channel_suppressed_channels
	words    []string // prefs.highlight_words, lower case
	keywords []string // all_notifications_prefs.global.global_keywords, a guess
	chans    map[string]chanPrefs
	dnd      dnd
}

type set map[string]bool

type chanPrefs struct {
	level string
	muted bool
	noAt  bool
}

type dnd struct {
	On        bool  `json:"dnd_enabled"`
	Start     int64 `json:"next_dnd_start_ts"`
	End       int64 `json:"next_dnd_end_ts"`
	Snooze    bool  `json:"snooze_enabled"`
	SnoozeEnd int64 `json:"snooze_endtime"`
}

// Load reads the prefs and dnd of client.userBoot, replacing what's held.
func (p *Prefs) Load(prefs, dnd jsontext.Value) {
	*p = Prefs{}
	var m map[string]jsontext.Value
	if jsonx.Unmarshal(prefs, &m) == nil {
		for name, v := range m {
			p.Set(name, v)
		}
	}
	p.SetDND(dnd)
}

// Set takes one pref, as boot lists them and pref_change sends them.
// Those it doesn't use are dropped.
func (p *Prefs) Set(name string, v jsontext.Value) {
	switch name {
	case "muted_channels":
		p.muted = ids(v)
	case "loud_channels":
		p.loud = ids(v)
	case "never_channels":
		p.never = ids(v)
	case "at_channel_suppressed_channels":
		p.noAt = ids(v)
	case "highlight_words":
		p.words = words(text(v))
	case "all_notifications_prefs":
		p.setAll(v)
	}
}

// setAll reads all_notifications_prefs, which Slack sends as a string of
// JSON. The shape is a guess from the web client's settings: global
// {global_desktop, global_keywords} and channels {id: {desktop, muted,
// suppress_at_channel}}.
func (p *Prefs) setAll(v jsontext.Value) {
	b := []byte(v)
	if v.Kind() == '"' {
		b = []byte(text(v))
	}
	var a struct {
		Global struct {
			Desktop  string `json:"global_desktop"`
			Keywords string `json:"global_keywords"`
		} `json:"global"`
		Channels map[string]struct {
			Desktop string `json:"desktop"`
			Muted   bool   `json:"muted"`
			NoAt    bool   `json:"suppress_at_channel"`
		} `json:"channels"`
	}
	if jsonx.Unmarshal(b, &a) != nil {
		return
	}
	p.level, p.keywords = norm(a.Global.Desktop), words(a.Global.Keywords)
	p.chans = make(map[string]chanPrefs, len(a.Channels))
	for id, c := range a.Channels {
		p.chans[id] = chanPrefs{norm(c.Desktop), c.Muted, c.NoAt}
	}
}

// SetDND takes the dnd object of boot, or the dnd_status of dnd_updated.
func (p *Prefs) SetDND(v jsontext.Value) {
	var d dnd
	if jsonx.Unmarshal(v, &d) == nil {
		p.dnd = d
	}
}

// Quiet says whether do not disturb, or a snooze, is on at now.
func (p *Prefs) Quiet(now time.Time) bool {
	u, d := now.Unix(), p.dnd
	return (d.Snooze && u < d.SnoozeEnd) || (d.On && d.Start <= u && u < d.End)
}

// Input is a message and what's around it, which is all Wants looks at.
type Input struct {
	Conv      string
	Self      string // your user id
	User      string // who wrote it
	Subtype   string
	Text      string
	Direct    bool // a DM or a group DM
	Reply     bool // in a thread
	Following bool // in a thread you started or replied in
	Looking   bool // you have this conversation open in a focused terminal
}

// Wants says whether the message should notify you: DMs, mentions, your
// keywords and replies in your threads do; yours, muted conversations,
// the one you're looking at and do not disturb don't.
func (p *Prefs) Wants(in Input, now time.Time) bool {
	c := p.chans[in.Conv]
	if in.Self == "" || in.User == in.Self || in.Looking || !spoken(in.Subtype) ||
		p.muted[in.Conv] || c.muted || p.Quiet(now) {
		return false
	}
	level := c.level
	switch {
	case level != "":
	case p.loud[in.Conv]:
		level = levelAll
	case p.never[in.Conv]:
		level = levelNone
	case p.level != "":
		level = p.level
	default:
		level = levelMentions
	}
	switch level {
	case levelNone:
		return false
	case levelAll:
		return true
	}
	return in.Direct || in.Following || p.mentions(in, c) || has(in.Text, p.words) || has(in.Text, p.keywords)
}

// mentions says whether the text names you, or everyone where that counts.
func (p *Prefs) mentions(in Input, c chanPrefs) bool {
	if strings.Contains(in.Text, "<@"+in.Self+">") || strings.Contains(in.Text, "<@"+in.Self+"|") {
		return true
	}
	if c.noAt || p.noAt[in.Conv] {
		return false
	}
	for _, s := range []string{"<!here", "<!channel", "<!everyone"} {
		if strings.Contains(in.Text, s+">") || strings.Contains(in.Text, s+"|") {
			return true
		}
	}
	return false
}

// spoken says whether a message of this subtype is someone saying
// something, rather than a join, a topic change or the like.
func spoken(subtype string) bool {
	switch subtype {
	case "", "bot_message", "me_message", "thread_broadcast", "file_share":
		return true
	}
	return false
}

// has says whether text holds any of words as a whole word, ignoring case.
func has(text string, words []string) bool {
	if len(words) == 0 {
		return false
	}
	text = strings.ToLower(text)
	for _, w := range words {
		for from := 0; ; {
			i := strings.Index(text[from:], w)
			if i < 0 {
				break
			}
			i += from
			end := i + len(w)
			if !edge(text[:i], true) && !edge(text[end:], false) {
				return true
			}
			from = end
		}
	}
	return false
}

// edge says whether a word continues into s, which is what comes before
// it (before) or after it.
func edge(s string, before bool) bool {
	if s == "" {
		return false
	}
	r := []rune(s)
	c := r[0]
	if before {
		c = r[len(r)-1]
	}
	return unicode.IsLetter(c) || unicode.IsDigit(c)
}

func norm(level string) string {
	switch strings.ToLower(level) {
	case "everything", "all":
		return levelAll
	case "mentions":
		return levelMentions
	case "nothing", "never", "none":
		return levelNone
	}
	return ""
}

// text is v as a string, or "" if it isn't one.
func text(v jsontext.Value) string {
	var s string
	if v.Kind() == '"' {
		_ = jsonx.Unmarshal(v, &s)
	}
	return s
}

// ids reads a comma separated list.
func ids(v jsontext.Value) set {
	out := set{}
	for _, id := range strings.Split(text(v), ",") {
		if id = strings.TrimSpace(id); id != "" {
			out[id] = true
		}
	}
	return out
}

func words(s string) []string {
	var out []string
	for _, w := range strings.Split(strings.ToLower(s), ",") {
		if w = strings.TrimSpace(w); w != "" {
			out = append(out, w)
		}
	}
	return out
}
