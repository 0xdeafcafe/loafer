// Package slacktest is a made-up Slack, for tests and loafer --demo: the
// methods loafer calls, served from an in-memory workspace, and the
// websocket, into which Post, Edit, Delete, Typing and Push write events
// as Slack would. It speaks Slack's shapes by encoding internal/slack's
// own types, so what loafer decodes is what it gets.
package slacktest

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/jsonx"
)

// Call is one API call the server took, its form without the token.
type Call struct {
	Method string
	Form   url.Values
}

type Server struct {
	*httptest.Server

	mu       sync.Mutex // everything below, and writes to the sockets, so events go out in order
	team     slack.Team
	users    []slack.User
	convs    map[string]*conv
	order    []string // conversation ids, as boot lists them
	sections []slack.Section
	emoji    map[string]string
	calls    []Call
	socks    map[*websocket.Conn]bool
	last     int64 // the newest ts handed out, in microseconds
	folk     folk  // presence, user groups (people.go)
	files    map[string]*file
	out      bool // signed out: every call is invalid_auth and the socket won't open
}

type conv struct {
	slack.Conversation
	msgs    []slack.Message            // oldest first
	replies map[string][]slack.Message // by thread_ts, oldest first
}

// New starts a server holding the workspace in workspace.go.
func New() *Server {
	s := &Server{convs: map[string]*conv{}, socks: map[*websocket.Conn]bool{}, files: map[string]*file{}}
	s.seed(time.Now())
	s.seedPeople()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", s.api)
	mux.HandleFunc("/upload/", s.upload)
	mux.HandleFunc("/files/", s.download)
	mux.HandleFunc("/", s.socket)
	s.Server = httptest.NewServer(mux)
	return s
}

// Close hangs up the sockets and stops the server.
func (s *Server) Close() {
	s.mu.Lock()
	for c := range s.socks {
		c.CloseNow()
	}
	s.mu.Unlock()
	s.Server.Close()
}

// Creds sign in to the server. There's nothing secret in them.
func (s *Server) Creds() slack.Creds {
	return slack.Creds{TeamID: s.team.ID, Team: s.team.Name, URL: s.URL + "/", UserID: Self,
		Token: "xoxc-slacktest", Cookie: "xoxd-slacktest"}
}

// Client is a client for the server. It points slack.Gateway here too, so
// only one server's websocket can be listened to at a time.
func (s *Server) Client() *slack.Client {
	slack.Gateway = "ws" + strings.TrimPrefix(s.URL, "http") + "/"
	return slack.New(s.Creds())
}

// Calls is every API call so far, in order.
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.calls)
}

// Messages is channel's history as the server holds it, oldest first.
func (s *Server) Messages(channel string) []slack.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.convs[channel]; c != nil {
		return slices.Clone(c.msgs)
	}
	return nil
}

// Push sends event, raw JSON, to every open socket.
func (s *Server) Push(event string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.push([]byte(event))
}

// Post has user say text in channel (in the thread at threadTS, if it's
// set), and tells the sockets.
func (s *Server) Post(channel, user, text, threadTS string) slack.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := slack.Message{Type: "message", TS: s.ts(), User: user, Text: text, ThreadTS: threadTS}
	if c := s.convs[channel]; c != nil {
		s.add(c, m)
	}
	s.pushMsg(channel, m)
	return m
}

// Edit changes the text of channel's message at ts, and tells the sockets.
func (s *Server) Edit(channel, ts, text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.edit(channel, ts, text) != nil
}

// Delete removes channel's message at ts, and tells the sockets.
func (s *Server) Delete(channel, ts string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.remove(channel, ts)
}

// Sockets is how many websockets are open.
func (s *Server) Sockets() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.socks)
}

// SetSignedOut has Slack stop taking the sign-in (hanging up the sockets),
// or take it again.
func (s *Server) SetSignedOut(out bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.out = out
	if out {
		for c := range s.socks {
			c.CloseNow()
			delete(s.socks, c)
		}
	}
}

// Typing says user is writing in channel.
func (s *Server) Typing(channel, user string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pushAny(map[string]any{"type": "user_typing", "channel": channel, "user": user})
}

// --- the API ---

func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	method := strings.TrimPrefix(r.URL.Path, "/api/")
	_ = r.ParseForm()
	f := r.PostForm
	f.Del("token")
	s.mu.Lock()
	s.calls = append(s.calls, Call{method, f})
	out, code := map[string]any(nil), "invalid_auth"
	if !s.out {
		out, code = s.serve(method, f)
	}
	s.mu.Unlock()
	if code != "" {
		out = map[string]any{"ok": false, "error": code}
	} else {
		out["ok"] = true
	}
	b, err := jsonx.Marshal(out, json.OmitZeroStructFields(true))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

// serve answers method, or names the error Slack would give. Call with
// the lock held.
func (s *Server) serve(method string, f url.Values) (map[string]any, string) {
	ch := f.Get("channel")
	c := s.convs[ch]
	switch method {
	case "auth.test":
		return map[string]any{"url": s.URL + "/", "team": s.team.Name, "team_id": s.team.ID,
			"user": "sam", "user_id": Self}, ""

	case "client.userBoot":
		var self slack.User
		var chans, ims []slack.Conversation
		for _, u := range s.users {
			if u.ID == Self {
				self = u
			}
		}
		for _, id := range s.order {
			c := s.convs[id]
			if c.IsIM {
				ims = append(ims, c.wire())
			} else {
				chans = append(chans, c.wire())
			}
		}
		return map[string]any{"self": self, "team": s.team, "channels": chans, "ims": ims}, ""

	case "client.counts":
		var chans, mpims, ims []slack.Snapshot
		for _, id := range s.order {
			c := s.convs[id]
			n := s.snapshot(c)
			switch {
			case c.IsIM:
				ims = append(ims, n)
			case c.IsMPIM:
				mpims = append(mpims, n)
			default:
				chans = append(chans, n)
			}
		}
		return map[string]any{"channels": chans, "mpims": mpims, "ims": ims}, ""

	case "users.list":
		return map[string]any{"members": s.users, "response_metadata": map[string]string{"next_cursor": ""}}, ""

	case "emoji.list":
		return map[string]any{"emoji": s.emoji}, ""

	case "users.channelSections.list":
		return map[string]any{"channel_sections": s.sections}, ""

	case "conversations.history":
		if c == nil {
			return nil, "channel_not_found"
		}
		limit, err := strconv.Atoi(f.Get("limit"))
		if err != nil || limit <= 0 {
			limit = 100
		}
		end := len(c.msgs)
		if latest := f.Get("latest"); latest != "" {
			end = sortSearch(c.msgs, latest)
		}
		begin := max(0, end-limit)
		page := slices.Clone(c.msgs[begin:end])
		slices.Reverse(page) // newest first, as Slack sends it
		return map[string]any{"messages": page, "has_more": begin > 0}, ""

	case "conversations.replies":
		if c == nil {
			return nil, "channel_not_found"
		}
		parent := s.find(ch, f.Get("ts"))
		if parent == nil {
			return nil, "thread_not_found"
		}
		return map[string]any{"messages": append([]slack.Message{*parent}, c.replies[parent.TS]...), "has_more": false}, ""

	case "chat.postMessage":
		if c == nil {
			return nil, "channel_not_found"
		}
		if strings.TrimSpace(f.Get("text")) == "" {
			return nil, "no_text"
		}
		m := slack.Message{Type: "message", TS: s.ts(), User: Self, Text: f.Get("text"), ThreadTS: f.Get("thread_ts")}
		s.add(c, m)
		s.pushMsg(ch, m) // the echo, which may well beat the response
		return map[string]any{"channel": ch, "ts": m.TS, "message": m}, ""

	case "chat.update":
		m := s.find(ch, f.Get("ts"))
		if m == nil {
			return nil, "message_not_found"
		}
		if m.User != Self {
			return nil, "cant_update_message"
		}
		m = s.edit(ch, m.TS, f.Get("text"))
		return map[string]any{"channel": ch, "ts": m.TS, "text": m.Text}, ""

	case "chat.delete":
		m := s.find(ch, f.Get("ts"))
		if m == nil {
			return nil, "message_not_found"
		}
		if m.User != Self {
			return nil, "cant_delete_message"
		}
		ts := m.TS
		s.remove(ch, ts)
		return map[string]any{"channel": ch, "ts": ts}, ""

	case "files.getUploadURLExternal", "files.completeUploadExternal":
		return s.uploads(method, f)

	case "conversations.mark":
		if c == nil {
			return nil, "channel_not_found"
		}
		ts := f.Get("ts")
		if ts > c.LastRead {
			c.LastRead = ts
		}
		kind := "channel_marked"
		switch {
		case c.IsIM:
			kind = "im_marked"
		case c.IsMPIM:
			kind = "mpim_marked"
		case c.IsPrivate:
			kind = "group_marked"
		}
		s.pushAny(map[string]any{"type": kind, "channel": ch, "ts": c.LastRead})
		return map[string]any{}, ""
	}
	if out, code, ok := s.serveFolk(method, f); ok {
		return out, code
	}
	return map[string]any{}, "" // the rest say ok and do nothing
}

// wire is c as boot lists it, latest and all.
func (c *conv) wire() slack.Conversation {
	w := c.Conversation
	if len(c.msgs) > 0 {
		w.Latest = []byte(strconv.Quote(c.msgs[len(c.msgs)-1].TS))
	}
	return w
}

// snapshot is c's read state as client.counts gives it: a DM counts every
// unread message as a mention, a channel only those naming you.
func (s *Server) snapshot(c *conv) slack.Snapshot {
	n := slack.Snapshot{ID: c.ID, LastRead: c.LastRead}
	if len(c.msgs) > 0 {
		n.Latest = c.msgs[len(c.msgs)-1].TS
	}
	for _, m := range c.msgs {
		if m.TS <= c.LastRead || m.User == Self {
			continue
		}
		n.HasUnreads = true
		if c.IsIM || c.IsMPIM || strings.Contains(m.Text, "<@"+Self+">") {
			n.MentionCount++
		}
	}
	return n
}

// edit changes the text of channel's message at ts and pushes
// message_changed. Call with the lock held.
func (s *Server) edit(channel, ts, text string) *slack.Message {
	m := s.find(channel, ts)
	if m == nil {
		return nil
	}
	prev := *m
	m.Text, m.Edited = text, &struct{}{}
	s.pushAny(map[string]any{"type": "message", "subtype": "message_changed", "hidden": true,
		"channel": channel, "ts": s.ts(), "message": *m, "previous_message": prev})
	return m
}

// remove lets go of channel's message at ts and pushes message_deleted.
// Call with the lock held.
func (s *Server) remove(channel, ts string) bool {
	c := s.convs[channel]
	if c == nil {
		return false
	}
	i := slices.IndexFunc(c.msgs, func(m slack.Message) bool { return m.TS == ts })
	if i < 0 {
		return false
	}
	c.msgs = slices.Delete(c.msgs, i, i+1)
	s.pushAny(map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true,
		"channel": channel, "ts": s.ts(), "deleted_ts": ts})
	return true
}

// add files m in c: a reply under its parent, anything else at the end.
// Call with the lock held.
func (s *Server) add(c *conv, m slack.Message) {
	if m.ThreadTS == "" || m.ThreadTS == m.TS {
		c.msgs = append(c.msgs, m)
		return
	}
	if p := s.find(c.ID, m.ThreadTS); p != nil {
		p.ThreadTS, p.ReplyCount, p.LatestReply = p.TS, p.ReplyCount+1, m.TS
		if !slices.Contains(p.ReplyUsers, m.User) {
			p.ReplyUsers = append(p.ReplyUsers, m.User)
		}
	}
	c.replies[m.ThreadTS] = append(c.replies[m.ThreadTS], m)
}

// find is channel's message at ts, in its history or a thread. Call with
// the lock held.
func (s *Server) find(channel, ts string) *slack.Message {
	c := s.convs[channel]
	if c == nil {
		return nil
	}
	if i := sortSearch(c.msgs, ts); i < len(c.msgs) && c.msgs[i].TS == ts {
		return &c.msgs[i]
	}
	for _, rs := range c.replies {
		for i := range rs {
			if rs[i].TS == ts {
				return &rs[i]
			}
		}
	}
	return nil
}

// sortSearch is where ts is, or would go, in msgs.
func sortSearch(msgs []slack.Message, ts string) int {
	i, _ := slices.BinarySearchFunc(msgs, ts, func(m slack.Message, ts string) int { return strings.Compare(m.TS, ts) })
	return i
}

// ts hands out a timestamp newer than any before it. Call with the lock
// held.
func (s *Server) ts() string {
	s.last = max(time.Now().UnixMicro(), s.last+1)
	return stamp(time.UnixMicro(s.last))
}

// --- the websocket ---

func (s *Server) socket(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	out := s.out
	s.mu.Unlock()
	if out {
		http.Error(w, "invalid_auth", http.StatusUnauthorized)
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"app.slack.com"}})
	if err != nil {
		return
	}
	defer c.CloseNow()
	s.mu.Lock()
	s.socks[c] = true
	write(c, []byte(`{"type":"hello"}`))
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.socks, c)
		s.mu.Unlock()
	}()
	for {
		_, b, err := c.Read(r.Context())
		if err != nil {
			return
		}
		var ping struct {
			Type string `json:"type"`
			ID   int    `json:"id"`
		}
		if jsonx.Unmarshal(b, &ping) == nil && ping.Type == "ping" {
			s.mu.Lock()
			write(c, fmt.Appendf(nil, `{"type":"pong","reply_to":%d}`, ping.ID))
			s.mu.Unlock()
		} else {
			s.frame(c, b)
		}
	}
}

// pushMsg sends m as a new message in channel. Call with the lock held.
func (s *Server) pushMsg(channel string, m slack.Message) {
	s.pushAny(struct {
		slack.Message
		Channel string `json:"channel"`
	}{m, channel})
}

// pushAny sends v as JSON. Call with the lock held.
func (s *Server) pushAny(v any) {
	b, err := jsonx.Marshal(v, json.OmitZeroStructFields(true))
	if err != nil {
		panic(err) // the server's own types: a bug here, not bad input
	}
	s.push(b)
}

func (s *Server) push(b []byte) {
	for c := range s.socks {
		if !write(c, b) {
			c.CloseNow()
			delete(s.socks, c)
		}
	}
}

func write(c *websocket.Conn, b []byte) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.Write(ctx, websocket.MessageText, b) == nil
}
