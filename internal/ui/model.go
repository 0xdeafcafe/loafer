// Package ui is loafer's screen: a bubbletea model drawing the store
// through canvas rows. It never waits: network and disk work runs in
// commands, and what they learn reaches the screen through the store.
package ui

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/obs"
	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/frame"
	"github.com/0xdeafcafe/photon/rows"
	"github.com/0xdeafcafe/photon/theme"
)

type focus uint8

const (
	onSide focus = iota
	onMsgs
	onCompose
)

type Model struct {
	st  *store.Store
	api *slack.Client
	ctx context.Context

	ground theme.Ground
	pal    Palette

	w, h  int
	focus focus

	side     []sideItem // the sidebar, flattened
	sideAt   int        // the selected item
	sideTop  int        // first item shown
	sideSeen uint64     // store version side was built from

	open      string   // the conversation shown
	newAt     string   // its last_read when opened, where the "new" line goes
	scroll    int      // rows scrolled up from the newest
	sel       string   // the message under the cursor; "" is past the newest
	follow    bool     // bring sel into view on the next frame
	fetching  bool     // older messages are on their way
	back, fwd []string // conversations visited, for alt+← and alt+→
	bar       jumper   // ctrl+k
	emo       emojiUI  // the reaction picker and :sm popup

	input    []rune
	cur      int
	drafts   map[string][]rune // what was left written in each conversation
	editing  string            // the message the composer is changing
	deleting string            // the message d was pressed on once

	live        string // connecting, live, offline, signed out
	flash       string
	flashErr    bool
	flashExpiry time.Time

	drawn     *rows.Cache[rowKey, []canvas.Row] // messages as drawn
	heights   map[string]int                    // each drawn message's rows, by ts
	index     rows.Index                        // the open window's messages' rows
	indexOf   indexKey
	rowsW     int
	rowsNames uint64

	frame     strings.Builder
	gate      frame.Gate
	showDebug bool
	debug     obs.Snapshot
}

// New makes the model; Init starts loading.
func New(ctx context.Context, st *store.Store, api *slack.Client) *Model {
	return &Model{
		st: st, api: api, ctx: ctx,
		ground: theme.Dark, pal: NewPalette(theme.Dark, Aubergine, false),
		live: "connecting", drafts: map[string][]rune{},
		drawn: rows.NewCache[rowKey, []canvas.Row](keepRows), heights: map[string]int{},
	}
}

// SignedOut says whether it quit because Slack stopped taking the sign-in.
func (m *Model) SignedOut() bool { return m.live == "signed out" }

type (
	storeMsg  struct{}
	bootedMsg struct{ err error }
	liveMsg   struct{ err error }
	openedMsg struct {
		conv string
		err  error
	}
	sentMsg     struct{ err error }
	flashOffMsg struct{}
	debugMsg    struct{}
)

func (m *Model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, m.waitStore(), m.boot())
}

func (m *Model) waitStore() tea.Cmd {
	return func() tea.Msg {
		select {
		case <-m.st.Changed():
			return storeMsg{}
		case <-m.ctx.Done():
			return nil
		}
	}
}

func (m *Model) boot() tea.Cmd {
	return func() tea.Msg { return bootedMsg{m.st.Boot(m.ctx, m.api)} }
}

// listen holds the websocket until loafer closes or Slack signs it out.
func (m *Model) listen() tea.Cmd {
	return func() tea.Msg { return liveMsg{m.st.Live(m.ctx, m.api)} }
}

// openConv shows conversation id, keeping what was being written in the
// one left. visit is the way in that remembers where you've been.
func (m *Model) openConv(id string) tea.Cmd {
	if m.editing != "" {
		m.cancelEdit()
	}
	if m.open != "" {
		m.drafts[m.open] = m.input
	}
	m.input = m.drafts[id]
	delete(m.drafts, id)
	m.cur = len(m.input)
	m.open, m.scroll, m.sel, m.deleting = id, 0, "", ""
	if i := slices.IndexFunc(m.side, func(it sideItem) bool { return it.conv == id }); i >= 0 {
		m.sideAt = i
	}
	m.st.Read(func(v store.View) {
		if c := v.Conv(id); c != nil {
			m.newAt = c.LastRead
		}
	})
	return func() tea.Msg {
		err := m.st.Open(m.ctx, m.api, id)
		return openedMsg{id, err}
	}
}

// markRead moves the open conversation's read marker to its newest
// message, here at once and at Slack in the background.
func (m *Model) markRead() tea.Cmd {
	var ts string
	m.st.Read(func(v store.View) {
		c, w := v.Conv(m.open), v.Window(m.open)
		if c != nil && w != nil && len(w.Msgs) > 0 && w.Msgs[len(w.Msgs)-1].TS > c.LastRead {
			ts = w.Msgs[len(w.Msgs)-1].TS
		}
	})
	if ts == "" {
		return nil
	}
	conv := m.open
	m.st.MarkRead(conv, ts)
	return func() tea.Msg {
		if err := m.api.Mark(m.ctx, conv, ts); err != nil {
			slog.Warn("mark", "conv", conv, "err", err)
		}
		return nil
	}
}

func (m *Model) send() tea.Cmd {
	text := strings.TrimSpace(string(m.input))
	if text == "" || m.open == "" {
		return nil
	}
	m.input, m.cur = nil, 0
	conv := m.open
	if ts := m.editing; ts != "" {
		m.editing = ""
		m.input = m.drafts[conv]
		m.cur = len(m.input)
		delete(m.drafts, conv)
		m.st.Edit(conv, ts, text)
		return func() tea.Msg {
			err := m.api.Update(m.ctx, conv, ts, text)
			if err != nil {
				_ = m.st.Refresh(m.ctx, m.api, conv) // put back what Slack has
			}
			return sentMsg{err}
		}
	}
	return func() tea.Msg {
		msg, err := m.api.Post(m.ctx, conv, text, "")
		if err == nil {
			m.st.Add(conv, msg) // before the websocket's copy, if it's slow
		}
		return sentMsg{err}
	}
}

func (m *Model) say(text string, isErr bool) tea.Cmd {
	m.flash, m.flashErr, m.flashExpiry = text, isErr, time.Now().Add(6*time.Second)
	return tea.Tick(6*time.Second, func(time.Time) tea.Msg { return flashOffMsg{} })
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.gate.Tick(msg) {
		return m, nil // draws what the wheel moved
	}
	_, cmd := m.update(msg)
	return m, tea.Batch(cmd, m.gate.Wheel(msg))
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	began := time.Now()
	defer func() {
		if d := time.Since(began); d > 75*time.Millisecond {
			slog.Warn("stall", "what", fmt.Sprintf("%T", msg), "ms", d.Milliseconds())
		}
	}()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		bg := theme.Of(msg.Color)
		m.ground = theme.Terminal(&bg, nil)
		m.pal = NewPalette(m.ground, Aubergine, false)
		m.drawn.Clear()
	case storeMsg:
		m.st.Read(func(v store.View) {
			if l := v.Link(); l != "" {
				m.live = l
			}
		})
		cmd := m.waitStore()
		if m.open != "" && m.scroll == 0 {
			cmd = tea.Batch(cmd, m.markRead()) // watching it come in is reading it
		}
		return m, cmd
	case bootedMsg:
		switch {
		case msg.err == nil:
		case slack.SignedOut(msg.err):
			m.live = "signed out"
			return m, tea.Quit // run signs in again and reopens
		default:
			// The socket boots again once it gets through.
			m.live = "offline"
			return m, tea.Batch(m.listen(), m.say("couldn't reach Slack: "+msg.err.Error(), true))
		}
		cmd := m.listen()
		if m.open == "" && len(m.side) > 0 {
			cmd = tea.Batch(cmd, m.openSelected())
		}
		return m, cmd
	case liveMsg:
		if slack.SignedOut(msg.err) {
			m.live = "signed out"
			return m, tea.Quit
		}
	case openedMsg:
		if msg.err != nil {
			return m, m.say("couldn't open: "+msg.err.Error(), true)
		}
		if msg.conv == m.open {
			return m, m.markRead()
		}
	case sentMsg:
		if msg.err != nil {
			return m, m.say("✗ couldn't send: "+msg.err.Error(), true)
		}
		return m, m.markRead()
	case reactedMsg:
		if msg.err != nil {
			return m, m.say("✗ couldn't react: "+msg.err.Error(), true)
		}
	case olderMsg:
		m.fetching = false
		if msg.err != nil {
			return m, m.say("couldn't fetch older messages: "+msg.err.Error(), true)
		}
	case flashOffMsg:
		if time.Now().After(m.flashExpiry) {
			m.flash = ""
		}
	case debugMsg:
		if m.showDebug {
			m.debug = obs.Take()
			return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return debugMsg{} })
		}
	case tea.PasteMsg:
		if m.bar.on {
			m.bar.query = append(m.bar.query, []rune(strings.ReplaceAll(msg.Content, "\n", " "))...)
			m.st.Read(m.buildJump)
		} else if m.focus == onCompose {
			m.insert(msg.Content)
		}
	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelUp {
			m.scroll += 3
		} else {
			m.scroll = max(0, m.scroll-3)
		}
	case tea.KeyPressMsg:
		return m, m.key(msg)
	default:
		m.gate.Keep()
	}
	return m, nil
}

func (m *Model) openSelected() tea.Cmd {
	if m.sideAt < len(m.side) && m.side[m.sideAt].conv != "" {
		return m.openConv(m.side[m.sideAt].conv)
	}
	return nil
}

func (m *Model) key(k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	if s != "d" && s != "delete" {
		m.deleting = ""
	}
	if m.bar.on && s != "ctrl+c" && s != "f12" {
		return m.jumpKey(k)
	}
	if cmd, ok := m.emojiKey(k, s); ok {
		return cmd
	}
	// Everywhere. ponytail: alt keys only; rush's ctrl+] leader for
	// terminals that eat alt comes with the keymap file.
	switch s {
	case "ctrl+c":
		return tea.Quit
	case "f12":
		m.showDebug = !m.showDebug
		if m.showDebug {
			m.debug = obs.Take()
			return tea.Tick(time.Second, func(time.Time) tea.Msg { return debugMsg{} })
		}
		return nil
	case "ctrl+alt+p":
		if dir := obs.Dump(); dir != "" {
			return m.say("profiling for 35s into "+dir, false)
		}
		return m.say("already profiling", false)
	case "tab":
		m.setFocus((m.focus + 1) % 3)
		return nil
	case "shift+tab":
		m.setFocus((m.focus + 2) % 3)
		return nil
	case "alt+up":
		return m.jump(-1, anyConv, "")
	case "alt+down":
		return m.jump(1, anyConv, "")
	case "alt+shift+up":
		return m.jump(-1, unreadConv, "nothing unread")
	case "alt+shift+down":
		return m.jump(1, unreadConv, "nothing unread")
	case "ctrl+n":
		return m.jump(1, needsYou, "nothing needs you")
	case "ctrl+k":
		// In the composer with text after the cursor, ctrl+k keeps its
		// readline meaning, as in rush.
		if m.focus == onCompose && m.cur < len(m.input) {
			m.input = m.input[:m.cur]
			return nil
		}
		m.openJump()
		return nil
	case "alt+left":
		return m.goBack()
	case "alt+right":
		return m.goForward()
	}
	switch m.focus {
	case onSide:
		return m.sideKey(s)
	case onMsgs:
		return m.msgsKey(s)
	}
	return m.composeKey(k, s)
}

// setFocus moves focus to f; the message cursor starts on the newest
// message, and goes when focus leaves.
func (m *Model) setFocus(f focus) {
	if f == onMsgs && m.focus != onMsgs {
		m.pick(by(-1))
	}
	if f != onMsgs {
		m.sel = ""
	}
	m.focus = f
}

func (m *Model) sideKey(s string) tea.Cmd {
	switch s {
	case "q":
		return tea.Quit
	case "up", "k":
		m.moveSide(-1)
	case "down", "j":
		m.moveSide(1)
	case "pgup":
		m.moveSide(-10)
	case "pgdown":
		m.moveSide(10)
	case "home", "g":
		m.sideAt = 0
		m.moveSide(1)
	case "end", "G":
		m.sideAt = len(m.side)
		m.moveSide(-1)
	case "n":
		return m.jump(1, unreadConv, "nothing unread")
	case "/":
		m.openJump()
	case "enter", "l", "right":
		m.setFocus(onCompose)
		if m.sideAt < len(m.side) {
			return m.visit(m.side[m.sideAt].conv)
		}
	case "i":
		m.setFocus(onCompose)
	}
	return nil
}

func (m *Model) msgsKey(s string) tea.Cmd {
	page := max(1, m.h/6) // ponytail: messages a page, near enough
	switch s {
	case "up", "k":
		return m.pick(by(-1))
	case "down", "j":
		return m.pick(by(1))
	case "pgup", "ctrl+u":
		return m.pick(by(-page))
	case "pgdown", "ctrl+d":
		return m.pick(by(page))
	case "ctrl+home", "home", "g":
		return m.pick(oldest)
	case "ctrl+end", "end", "G":
		return m.pick(newest)
	case "{":
		return m.pick(prevGroup)
	case "}":
		return m.pick(nextGroup)
	case "n":
		return m.toNew()
	case "@":
		return m.toMention()
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		return m.reactNth(int(s[0] - '0'))
	case "r", "e", "c", "l", "o", "d", "delete":
		msg, ok := m.selected()
		if !ok {
			return m.say("pick a message first (↑)", false)
		}
		switch s {
		case "r":
			m.reactPicker(msg)
			return nil
		case "e":
			return m.edit(msg)
		case "d", "delete":
			return m.remove(msg)
		case "c":
			return m.copyText(msg)
		case "l":
			return tea.Batch(tea.SetClipboard(m.permalink(msg)), m.say("copied a link to the message", false))
		}
		return m.openLink(msg)
	case "esc", "left", "h":
		if m.sel != "" {
			m.sel, m.scroll = "", 0
			return nil
		}
		m.setFocus(onSide)
	case "i", "a", "enter":
		m.setFocus(onCompose)
	}
	return nil
}

func (m *Model) composeKey(k tea.KeyPressMsg, s string) tea.Cmd {
	switch s {
	case "esc":
		if m.editing != "" {
			m.cancelEdit()
			return nil
		}
		m.setFocus(onMsgs)
	case "enter":
		return m.send()
	case "up":
		if len(m.input) == 0 && m.editing == "" {
			if msg, ok := m.lastOwn(); ok {
				return m.edit(msg)
			}
		}
	case "shift+enter", "alt+enter", "ctrl+j":
		m.insert("\n")
	case "backspace":
		if m.cur > 0 {
			m.input = append(m.input[:m.cur-1], m.input[m.cur:]...)
			m.cur--
		}
	case "alt+backspace", "ctrl+w":
		i := m.cur
		for i > 0 && m.input[i-1] == ' ' {
			i--
		}
		for i > 0 && m.input[i-1] != ' ' && m.input[i-1] != '\n' {
			i--
		}
		m.input = append(m.input[:i], m.input[m.cur:]...)
		m.cur = i
	case "delete":
		if m.cur < len(m.input) {
			m.input = append(m.input[:m.cur], m.input[m.cur+1:]...)
		}
	case "left":
		m.cur = max(0, m.cur-1)
	case "right":
		m.cur = min(len(m.input), m.cur+1)
	case "home", "ctrl+a":
		m.cur = 0
	case "end", "ctrl+e":
		m.cur = len(m.input)
	case "pgup":
		m.scroll += max(1, m.h/2)
	case "pgdown":
		m.scroll = max(0, m.scroll-m.h/2)
	default:
		if k.Text != "" {
			m.insert(k.Text)
		}
	}
	return nil
}

func (m *Model) insert(s string) {
	r := []rune(s)
	m.input = append(m.input[:m.cur], append(r, m.input[m.cur:]...)...)
	m.cur += len(r)
}

// moveSide moves the sidebar selection d items, onto the nearest
// conversation (headings aren't stopped on).
func (m *Model) moveSide(d int) {
	if len(m.side) == 0 {
		return
	}
	i := min(max(m.sideAt+d, 0), len(m.side)-1)
	dir := 1
	if d < 0 {
		dir = -1
	}
	for _, dir := range []int{dir, -dir} {
		for j := i; j >= 0 && j < len(m.side); j += dir {
			if m.side[j].conv != "" {
				m.sideAt = j
				return
			}
		}
	}
}
