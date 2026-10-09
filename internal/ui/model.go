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
	onThread // the thread pane's messages
	onReply  // the thread pane's box
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
	find      finder   // ctrl+f
	th        threadPane
	tabs      tabState // DMs, Activity, Later and Claude (tabs.go)
	att       attachState

	input    []rune
	ments    []mention // the runs of input that are mentions
	cur      int
	pop      popup            // @ and # completion
	drafts   map[string]draft // what was left written in each conversation
	editing  string           // the message the composer is changing
	deleting string           // the message d was pressed on once

	al     alerts // notifications and the typing line (alerts.go)
	claude claude // the Claude tab (claude.go)

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
		live: "connecting", drafts: map[string]draft{},
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
	return tea.Batch(tea.RequestBackgroundColor, m.waitStore(), m.waitNotes(), m.boot(), m.startPics())
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
	m.dropThread()
	if m.editing != "" {
		m.cancelEdit()
	}
	if m.open != "" {
		m.keep(m.open)
	}
	m.restore(id)
	m.open, m.scroll, m.sel, m.deleting = id, 0, "", ""
	m.watch()
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
	if cmd, ok := m.sendFiles(); ok {
		return cmd
	}
	text := strings.TrimSpace(encode(m.input, m.ments))
	if text == "" || m.open == "" {
		return nil
	}
	m.load(nil, nil)
	conv := m.open
	if ts := m.editing; ts != "" {
		m.editing = ""
		m.restore(conv)
		m.st.Edit(conv, ts, text)
		return func() tea.Msg {
			err := m.api.Update(m.ctx, conv, ts, text)
			if err != nil {
				_ = m.st.Refresh(m.ctx, m.api, conv) // put back what Slack has
			}
			return sentMsg{err}
		}
	}
	if m.th.in {
		return m.reply(conv, text)
	}
	return func() tea.Msg {
		msg, err := m.api.Post(m.ctx, conv, text, "")
		if err == nil {
			m.st.Add(conv, msg) // before the websocket's copy, if it's slow
			if err := m.st.Newest(m.ctx, m.api, conv); err != nil {
				slog.Warn("newest", "conv", conv, "err", err) // left back where a search went
			}
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
	if cmd, ok := m.onPics(msg); ok {
		return m, cmd
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		bg := theme.Of(msg.Color)
		m.ground = theme.Terminal(&bg, nil)
		m.pal = NewPalette(m.ground, Aubergine, false)
		m.drawn.Clear()
		m.claude.redraw()
	case storeMsg:
		m.st.Read(func(v store.View) {
			if l := v.Link(); l != "" {
				m.live = l
			}
		})
		cmd := tea.Batch(m.waitStore(), m.fetchTabs(), m.readIfWatching()) // watching it come in is reading it
		return m, tea.Batch(cmd, m.watchTyping(), m.markThread())
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
			return m, m.readIfWatching() // not if you've scrolled up meanwhile
		}
	case tabMsg:
		return m, m.tabDone(msg)
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
			return m, m.say("couldn't fetch messages: "+msg.err.Error(), true)
		}
	case searchTickMsg, searchedMsg, foundMsg:
		return m, m.searched(msg)
	case threadMsg:
		return m, m.threaded(msg)
	case fileMsg, listMsg, upMsg, savedMsg:
		return m, m.attachUpdate(msg)
	case askedMsg, claudeMsg, caughtUpMsg, rushDoneMsg:
		return m, m.claudeUpdate(msg)
	case tea.FocusMsg, tea.BlurMsg, noteMsg, flushMsg, typingMsg:
		cmd := m.alert(msg)
		if _, back := msg.(tea.FocusMsg); back {
			cmd = tea.Batch(cmd, m.readIfWatching())
		}
		return m, cmd
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
		} else if m.find.on {
			m.find.query = append(m.find.query, []rune(strings.ReplaceAll(msg.Content, "\n", " "))...)
			return m, m.edited()
		} else if m.tabs.on == tabClaude {
			m.claude.insert(msg.Content)
		} else if cmd, ok := m.pastedFiles(msg.Content); ok {
			return m, cmd
		} else if m.focus == onCompose {
			m.insert(msg.Content)
			m.refreshPop()
		} else if m.focus == onReply {
			m.inThread(func() { m.insert(msg.Content); m.refreshPop() })
		}
	case tea.MouseWheelMsg:
		m.wheel(msg)
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
	if m.emo.pick.on && s != "ctrl+c" && s != "f12" {
		return m.reactKey(k)
	}
	if m.find.on && s != "ctrl+c" && s != "f12" {
		return m.searchKey(k)
	}
	if cmd, ok := m.attachKey(k, s); ok {
		return cmd
	}
	if m.focus == onCompose && m.pop.on && m.popKey(s) {
		return nil
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
		m.setFocus(m.next(1))
		return nil
	case "shift+tab":
		m.setFocus(m.next(-1))
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
			m.splice(m.cur, len(m.input), nil)
			return nil
		}
		m.openJump()
		return nil
	case "ctrl+f":
		m.openSearch()
		return nil
	case "alt+left":
		return m.goBack()
	case "alt+right":
		return m.goForward()
	case "alt+1", "alt+2", "alt+3", "alt+4", "alt+5":
		return m.setTab(tabID(s[4] - '1'))
	case "alt+c":
		return m.claudeHere()
	case "ctrl+end": // the conversation's newest, from anywhere but the thread
		if m.focus < onThread && m.tabs.on != tabClaude {
			cmd := m.pick(newest)
			return tea.Batch(cmd, m.readIfWatching())
		}
	}
	if m.tabs.on == tabClaude {
		return m.claudeKey(k)
	}
	switch m.focus {
	case onSide:
		if m.tabs.on != tabHome {
			return m.tabKey(s)
		}
		return m.sideKey(s)
	case onMsgs:
		return m.msgsKey(s)
	case onThread, onReply:
		return m.threadKey(k, s)
	}
	cmd := m.composeKey(k, s)
	m.refreshPop()
	return cmd
}

// setFocus moves focus to f; the message cursor starts on the newest
// message, and goes when focus leaves.
func (m *Model) setFocus(f focus) {
	m.uncover(f)
	m.threadFocus(f)
	if f == onMsgs && m.focus != onMsgs && m.sel == "" { // after an edit, stay on what was edited
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
	case "/":
		m.openSearch()
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
			return m.pick(newest)
		}
		m.setFocus(onSide)
	case "t", "right", "enter":
		return m.threadAt(s)
	case "D", "O":
		return m.save(s == "O")
	case "a":
		return m.claudeAbout()
	case "i":
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
		m.vertical(-1)
	case "shift+enter", "alt+enter", "ctrl+j":
		m.insert("\n")
	case "backspace":
		if m.cur > 0 {
			m.splice(m.cur-1, m.cur, nil)
		} else {
			m.dropFile()
		}
	case "ctrl+o":
		return m.askFile()
	case "alt+backspace", "ctrl+w":
		m.splice(m.wordLeft(m.cur), m.cur, nil)
	case "ctrl+u":
		m.splice(m.lineStart(m.cur), m.cur, nil)
	case "delete":
		if m.cur < len(m.input) {
			m.splice(m.cur, m.cur+1, nil)
		}
	case "left":
		m.cur = max(0, m.cur-1)
	case "right":
		m.cur = min(len(m.input), m.cur+1)
	case "ctrl+left", "alt+b":
		m.cur = m.wordLeft(m.cur)
	case "ctrl+right", "alt+f":
		m.cur = m.wordRight(m.cur)
	case "home", "ctrl+a":
		m.cur = m.lineStart(m.cur)
	case "end", "ctrl+e":
		m.cur = m.lineEnd(m.cur)
	case "down":
		m.vertical(1)
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

func (m *Model) insert(s string) { m.splice(m.cur, m.cur, []rune(s)) }

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
