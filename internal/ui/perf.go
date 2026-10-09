package ui

import (
	"cmp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/fuzzy"
)

// What a frame would otherwise work out again each time (docs/perf.md):
// the sidebar, built again only when its layout changes; the counts over
// it, once a change; each of its rows, drawn once until it changes; and
// the rows the panes are joined into, kept from frame to frame.

type sideCache struct {
	layout                  uint64 // the store's Layout the sidebar was built from
	mentions, unread, needs int    // over the sidebar, as of sideSeen
	pal                     Palette
	rows                    map[sideKey]canvas.Row
	body                    []canvas.Row
}

// sideKey is everything a sidebar row shows.
type sideKey struct {
	id, name              string
	names                 uint64
	kind                  store.Kind
	presence              string // a DM's user's (Presence); "" for the rest
	muted, unread         bool
	mentions              int
	selected, open, hover bool
	w                     int
}

// refreshSide brings the sidebar and its counts up to the store.
func (m *Model) refreshSide(v store.View) {
	if m.st.Version() == m.sideSeen {
		return
	}
	m.sideSeen = m.st.Version()
	if l := v.Layout(); l != m.sc.layout || len(m.side) == 0 || m.foldStale(v) {
		m.buildSide(v)
		m.sc.layout = l
	}
	m.sc.mentions, m.sc.unread, m.sc.needs = 0, 0, 0
	for _, it := range m.side {
		c := v.Conv(it.conv)
		if c == nil {
			continue
		}
		if !c.Muted {
			m.sc.mentions += c.Mentions
			if c.Unread {
				m.sc.unread++
			}
		}
		if needsYou(c) {
			m.sc.needs++
		}
	}
}

// foldStale says whether what collapsed sections show still holds: they
// surface the open and the unread (manage.go), which Layout doesn't
// track. The sidebar keeps the sections' order, so the built list is
// walked beside them.
func (m *Model) foldStale(v store.View) bool {
	at := 0
	for _, sec := range v.Sidebar() {
		if !sec.Collapsed {
			at += 1 + len(sec.Convs) // the heading and all of them
			continue
		}
		at++ // the heading
		for _, id := range sec.Convs {
			c := v.Conv(id)
			if c == nil || (id != m.open && !unreadConv(c)) {
				continue
			}
			if at >= len(m.side) || m.side[at].conv != id {
				return true
			}
			at++
		}
	}
	return false
}

// keptSideRow is sideRow, drawn once for as long as what it shows holds.
func (m *Model) keptSideRow(v store.View, c *store.Conv, w int, selected, open bool) canvas.Row {
	if m.sc.pal != m.pal || len(m.sc.rows) > 4*len(m.side)+64 {
		m.sc.pal, m.sc.rows = m.pal, map[sideKey]canvas.Row{}
	}
	presence := ""
	if c.Kind == store.IM {
		presence = v.Presence(c.User)
	}
	k := sideKey{c.ID, c.Name, v.Names(), c.Kind, presence, c.Muted, c.Unread, c.Mentions, selected, open, selected && m.focus == onSide, w}
	r, ok := m.sc.rows[k]
	if !ok {
		r = m.sideRow(v, c, w, selected, open)
		m.sc.rows[k] = r
	}
	return r
}

// counted is how many of msgs m.index still counts rightly for key: all
// it counted, when all that's changed is newer messages after them, as
// they arrive; else none, and they're counted again.
func (m *Model) counted(key indexKey, msgs []slack.Message) int {
	old := m.indexOf
	if old.conv != key.conv || old.first != key.first || old.newAt != key.newAt || old.w != key.w || old.names != key.names ||
		old.n == 0 || old.n > len(msgs) || m.index.Len() != old.n || msgs[old.n-1].TS != old.last {
		return 0
	}
	return old.n
}

// match is fuzzy.Match, asked only of what could match: the lists it
// runs over (20,000 people, every emoji) mostly can't, and finding that
// out costs fuzzy.Match a few allocations a name.
func match(q, s string) (int, []int, bool) {
	if !could(q, s) {
		return 0, nil, false
	}
	if q == "" {
		return 0, nil, true // as fuzzy.Match says, without its work
	}
	return fuzzy.Match(q, s)
}

// could says whether each word of q is in s, its letters in order, case
// aside, which fuzzy.Match needs before it'll match. It allocates nothing.
func could(q, s string) bool {
	for w := range strings.FieldsSeq(q) {
		for _, r := range s {
			if w == "" {
				break
			}
			c, n := utf8.DecodeRuneInString(w)
			if r == c || unicode.ToLower(r) == unicode.ToLower(c) {
				w = w[n:]
			}
		}
		if w != "" {
			return false
		}
	}
	return true
}

// compareFold is strings.Compare of the two lowered, without lowering
// them into new strings.
func compareFold(a, b string) int {
	for a != "" && b != "" {
		ra, na := utf8.DecodeRuneInString(a)
		rb, nb := utf8.DecodeRuneInString(b)
		if ra, rb = unicode.ToLower(ra), unicode.ToLower(rb); ra != rb {
			return cmp.Compare(ra, rb)
		}
		a, b = a[na:], b[nb:]
	}
	return cmp.Compare(len(a), len(b))
}

// top is the first k of s as slices.SortFunc would order it, in order,
// without sorting the rest: a popup shows 8 of 20,000. It reuses s.
func top[T any](s []T, k int, cmp func(a, b T) int) []T {
	n := 0
	if k <= 0 {
		return s[:0]
	}
	for _, x := range s {
		if n == k && cmp(x, s[k-1]) >= 0 {
			continue
		}
		i := min(n, k-1)
		n = min(n+1, k)
		for ; i > 0 && cmp(x, s[i-1]) < 0; i-- {
			s[i] = s[i-1]
		}
		s[i] = x
	}
	return s[:n]
}

// join puts the sidebar, a rule and the panes side by side, into rows
// kept from the last frame (nothing holds a frame's rows past it).
func (m *Model) join(side, panes []canvas.Row, h int) []canvas.Row {
	rule := canvas.T("│", m.pal.Main.Faint)
	h = max(h, len(side), len(panes))
	if cap(m.sc.body) < h {
		m.sc.body = make([]canvas.Row, h)
	}
	body := m.sc.body[:h]
	for i := range body {
		r := body[i][:0]
		if i < len(side) {
			r = append(r, side[i]...)
		}
		r = append(r, rule)
		if i < len(panes) {
			r = append(r, panes[i]...)
		}
		body[i] = r
	}
	return body
}
