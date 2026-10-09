package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/theme"
)

// The welcome is what a cold start shows while there's nothing cached to
// draw: the logo, a hello, boot's steps as they really finish, and the
// keys worth knowing. It goes once the sidebar can draw (docs/ui.md,
// Welcome). It's drawn again only when what it shows changes.

type welcome struct {
	on   bool
	hold bool   // shown on purpose (LOAFER_WELCOME): it waits for a key once ready
	err  string // why boot didn't get through, while it keeps trying
	lace int    // the logo's frame while it's drawn in; laceN once it's still

	logoBG theme.RGB
	logo   [][]canvas.Row // lace's frames then the still one, on logoBG

	key  welKey
	rows []canvas.Row
}

// welKey is everything the welcome shows; the same key, the same rows.
type welKey struct {
	w, h, lace int
	p          store.Progress
	team, name string
	err        string
	out        bool
	bg         theme.RGB
}

type welcomeTickMsg struct{}

const (
	laceN     = 18 // the lace animation's frames (tools/logo)
	laceEvery = 60 * time.Millisecond
)

// Welcome shows the welcome until the workspace is ready: for a cold
// start, with no cache. hold keeps it up, once ready, until a key.
func (m *Model) Welcome(hold bool) { m.wel.on, m.wel.hold = true, hold }

// welcomeInit starts the logo drawing itself in.
func (m *Model) welcomeInit() tea.Cmd {
	if !m.wel.on {
		return nil
	}
	return tea.Tick(laceEvery, func(time.Time) tea.Msg { return welcomeTickMsg{} })
}

// ready says whether the sidebar can draw: you, your conversations, their
// counts and the sections they go in.
func ready(p store.Progress) bool {
	return p.Has(store.StepBoot) && !p.Bad(store.StepBoot) && p.Has(store.StepCounts) && !p.Bad(store.StepCounts) && p.Has(store.StepSections)
}

func (m *Model) progress() (p store.Progress) {
	m.st.Read(func(v store.View) { p = v.Progress() })
	return p
}

// onWelcome takes what the welcome deals with while it's up.
func (m *Model) onWelcome(msg tea.Msg) (tea.Cmd, bool) {
	if !m.wel.on {
		return nil, false
	}
	switch msg := msg.(type) {
	case welcomeTickMsg:
		if m.wel.lace++; m.wel.lace < laceN {
			return m.welcomeInit(), true
		}
		return nil, true
	case storeMsg:
		p := m.progress()
		if p.Has(store.StepBoot) && !p.Bad(store.StepBoot) {
			m.wel.err = ""
		}
		if !m.wel.hold && ready(p) && p.Done == 1<<store.Steps-1 {
			// Everything's in: the app, as the store's change would have it.
			return tea.Batch(m.handOver(), func() tea.Msg { return storeMsg{} }), true
		}
		return m.waitStore(), true
	case bootedMsg:
		switch {
		case msg.err == nil:
		case slack.SignedOut(msg.err):
			m.live = "signed out" // a key quits, and run signs in again
			return nil, true
		default:
			m.wel.err = msg.err.Error()
		}
		return nil, false
	case tea.KeyPressMsg:
		switch {
		case msg.String() == "ctrl+c", m.live == "signed out":
			return tea.Quit, true
		case ready(m.progress()):
			return m.handOver(), true
		}
		m.gate.Keep()
		return nil, true
	case tea.MouseMsg, tea.PasteMsg:
		m.gate.Keep()
		return nil, true
	}
	return nil, false
}

// handOver puts the app up in the welcome's place, opening the first
// conversation if boot hasn't yet.
func (m *Model) handOver() tea.Cmd {
	m.wel = welcome{}
	m.st.Read(m.refreshSide)
	if m.open == "" && len(m.side) > 0 && !m.hidden() {
		return m.openSelected()
	}
	return nil
}

// --- drawing ---

func (m *Model) welcomeRows() []canvas.Row {
	k := welKey{w: m.w, h: m.h, lace: m.wel.lace, err: m.wel.err, out: m.live == "signed out", bg: m.pal.Side.Ground}
	m.st.Read(func(v store.View) {
		k.p, k.team = v.Progress(), v.Team().Name
		if self := v.Self(); self != "" {
			if p := v.Person(self); p.Name != self {
				k.name = p.Name
			}
		}
	})
	if k.team == "" && m.api != nil {
		k.team = m.api.Team()
	}
	if k == m.wel.key && m.wel.rows != nil {
		return m.wel.rows
	}
	m.wel.key, m.wel.rows = k, m.drawWelcome(k)
	return m.wel.rows
}

// A welPart is some rows, centred one by one or as a block.
type welPart struct {
	rows  []canvas.Row
	block bool
}

func (m *Model) drawWelcome(k welKey) []canvas.Row {
	ink := m.pal.Side
	red := ink.Text.Fg(m.pal.Red.FG)
	logo := welPart{rows: m.logoFrame(k.lace)}

	title := canvas.Row{canvas.T("welcome to ", ink.Text), canvas.T("loafer", m.pal.Brand.With(canvas.Bold|canvas.Italic))}
	if k.name != "" {
		title = append(title, canvas.T(", "+k.name, ink.Text))
	}
	team := k.team
	if team == "" {
		team = "your workspace"
	}
	hello := welPart{rows: []canvas.Row{title}}
	intro := welPart{rows: []canvas.Row{title}}
	for _, s := range []string{team + " is new to loafer, so it's fetching it all once.", "next time it opens from the cache, straight away."} {
		intro.rows = append(intro.rows, canvas.Wrap(canvas.Row{canvas.T(s, ink.Sub)}, max(10, k.w-2))...)
	}

	steps := welPart{block: true}
	step := func(state int, label, detail string) { // state: 0 waiting, 1 under way, 2 done, 3 failed
		glyph, lab := canvas.T("· ", ink.Faint), ink.Faint
		switch state {
		case 1:
			glyph, lab = canvas.T("◌ ", ink.Dim), ink.Sub
		case 2:
			glyph, lab = canvas.T("✓ ", m.pal.SideGreen), ink.Text
		case 3:
			glyph, lab = canvas.T("✗ ", red), ink.Text
		}
		r := canvas.Row{glyph, canvas.T(fmt.Sprintf("%-22s", label), lab)}
		if detail != "" {
			r = append(r, canvas.T(detail, ink.Dim))
		}
		steps.rows = append(steps.rows, r)
	}
	p := k.p
	state := func(s store.Step, after bool) int {
		switch {
		case p.Bad(s):
			return 3
		case p.Has(s):
			return 2
		case after:
			return 1
		}
		return 0
	}
	switch {
	case k.out:
		step(3, "signing in", "slack didn't take it")
	case p.Done&^p.Failed != 0:
		step(2, "signed in", "")
	default:
		step(1, "signing in", "")
	}
	bootOK := p.Has(store.StepBoot) && !p.Bad(store.StepBoot)
	detail := func(s store.Step, n int, what string) string {
		switch {
		case p.Bad(s) && (s == store.StepBoot || s == store.StepCounts):
			return "not yet, trying again" // the socket boots again when it gets through
		case p.Bad(s):
			return "couldn't, carrying on"
		case n > 0:
			return count(n) + " " + what
		}
		return ""
	}
	step(state(store.StepBoot, !k.out), "the workspace", detail(store.StepBoot, p.Convs, "conversations"))
	people, n := "in all", p.People
	if !p.Has(store.StepPeople) {
		people = "so far"
		if n < 2 {
			n = 0 // only you, from boot: users.list hasn't answered yet
		}
	}
	step(state(store.StepPeople, !k.out), "people", detail(store.StepPeople, n, people))
	step(state(store.StepSections, !k.out), "channels and sections", detail(store.StepSections, 0, ""))
	step(state(store.StepCounts, bootOK), "unread counts", detail(store.StepCounts, 0, ""))
	step(state(store.StepEmoji, !k.out), "emoji", detail(store.StepEmoji, p.Emoji, "of your own"))

	keys := [][3]string{ // the key, what it does, and that in short
		{"ctrl+k", "jump to anything", "jump"},
		{"ctrl+n", "the next thing that needs you", "what needs you"},
		{"tab", "sidebar, messages, the box", "move"},
		{"alt+1…5", "home, dms, activity, later, claude", "tabs"},
		{".", "all you can do with a message", "actions"},
	}
	full := welPart{block: true}
	for _, kv := range keys {
		full.rows = append(full.rows, canvas.Row{canvas.T(fmt.Sprintf("%-9s", kv[0]), ink.Text.With(canvas.Bold)), canvas.T(kv[1], ink.Dim)})
	}
	var line canvas.Row
	for i, kv := range keys[:3] {
		if i > 0 {
			line = append(line, canvas.T(" · ", ink.Faint))
		}
		line = append(line, canvas.T(kv[0], ink.Text.With(canvas.Bold)), canvas.T(" "+kv[2], ink.Dim))
	}
	compact := welPart{rows: canvas.Wrap(line, max(10, k.w-2))}

	var foot canvas.Row
	all := p.Done == 1<<store.Steps-1
	switch {
	case k.out:
		foot = canvas.Row{canvas.T("✗ slack signed you out · ", red), canvas.T("any key", ink.Text.With(canvas.Bold)), canvas.T(" to sign in again", ink.Dim)}
	case k.err != "":
		foot = canvas.Row{canvas.T("◌ couldn't reach slack, trying again · ", m.pal.SideYellow), canvas.T("ctrl+c", ink.Text.With(canvas.Bold)), canvas.T(" quits", ink.Dim)}
	case ready(p):
		foot = canvas.Row{canvas.T("any key", ink.Text.With(canvas.Bold)), canvas.T(" to start", ink.Dim)}
		if !all {
			foot = append(foot, canvas.T(" · the rest keeps coming", ink.Faint))
		}
	default:
		foot = canvas.Row{canvas.T("it opens by itself when it's ready · ", ink.Faint), canvas.T("ctrl+c", ink.Dim.With(canvas.Bold)), canvas.T(" quits", ink.Faint)}
	}
	footer := welPart{rows: canvas.Wrap(foot, max(10, k.w-2))}

	// The roomiest layout that fits: the keys in full, then on a line,
	// then without the logo, then without the intro, then without keys.
	var parts []welPart
	for _, try := range [][]welPart{
		{logo, intro, steps, full, footer},
		{logo, intro, steps, compact, footer},
		{intro, steps, compact, footer},
		{hello, steps, compact, footer},
		{hello, steps, footer},
	} {
		if parts = try; height(parts) <= k.h {
			break
		}
	}
	fill := canvas.Style{}.Bg(ink.Ground)
	out := make([]canvas.Row, 0, k.h)
	blank := canvas.Row{canvas.T(strings.Repeat(" ", k.w), fill)}
	for range max(0, (k.h-height(parts))/2) {
		out = append(out, blank)
	}
	for i, pt := range parts {
		if i > 0 {
			out = append(out, blank)
		}
		bw := 0
		for _, r := range pt.rows {
			bw = max(bw, r.Width())
		}
		for _, r := range pt.rows {
			w := bw
			if !pt.block {
				w = r.Width()
			}
			pad := canvas.Row{canvas.T(strings.Repeat(" ", max(0, (k.w-w)/2)), fill)}
			out = append(out, canvas.Fit(append(pad, r...), k.w, fill))
		}
	}
	for len(out) < k.h {
		out = append(out, blank)
	}
	return out[:k.h]
}

// height is the parts' rows, with a blank between each.
func height(parts []welPart) int {
	n := -1
	for _, p := range parts {
		n += len(p.rows) + 1
	}
	return n
}

// count is n with thousands marked: 12,480.
func count(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// WelcomeIfCold shows the welcome if the store has nothing to draw: a
// first run, or a workspace just signed in to. hold shows it whatever the
// cache, waiting for a key once ready (LOAFER_WELCOME=1).
func (m *Model) WelcomeIfCold(hold bool) {
	if hold || m.st.Cold() {
		m.Welcome(hold)
	}
}
