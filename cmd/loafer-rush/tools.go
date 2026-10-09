package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/hex"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/loafer/internal/ui"
	"github.com/0xdeafcafe/photon/jsonx"
)

// most is the most Slack text one answer carries, about 8k tokens.
const most = 32 << 10

// maxDraft is the most a draft may be, as loafer draft takes it.
const maxDraft = 40 << 10

const noSend = " It never sends, reacts or marks anything read."

var tools = []map[string]any{
	{"name": "slack_search", "description": "Search the user's Slack messages (search.messages), 20 a page. Slack's modifiers work: in:#channel, from:@person, before:2026-10-01, is:thread. Each hit says where it was and has its link." + noSend,
		"inputSchema": schema(map[string]string{"query": "What to search for.", "page": "Which page, from 1."}, "query")},
	{"name": "slack_read", "description": "Read a Slack conversation's recent messages, or a thread whole, as plain text with names resolved." + noSend,
		"inputSchema": schema(map[string]string{
			"conversation": "#channel, @person (your DM with them), a channel or DM id, or a message's link (which reads its thread).",
			"thread":       "A thread to read in it: its parent's ts or link.",
			"limit":        "How many of the latest messages, 1 to 200; 50 if left out.",
		}, "conversation")},
	{"name": "slack_unread", "description": "What's unread in the user's Slack: each conversation with unread messages, and how many mention them, mentions and DMs first." + noSend,
		"inputSchema": schema(map[string]string{})},
	{"name": "slack_who", "description": "Look a Slack user up: their name, handle, title, timezone and status, and whether the user has a DM with them." + noSend,
		"inputSchema": schema(map[string]string{"person": "@handle, a name or part of one, or a user id."}, "person")},
	{"name": "slack_draft", "description": "Put text into loafer's composer as a draft, for a conversation or a thread, after anything already written there. It never sends: the user reads it, changes it and sends it, or doesn't. There is no tool that sends.",
		"inputSchema": schema(map[string]string{
			"conversation": "#channel, @person, a conversation id, or a message's link (which drafts in its thread).",
			"thread":       "A thread to draft in: its parent's ts or link.",
			"text":         "The draft, as the user would write it.",
		}, "conversation", "text")},
}

func schema(props map[string]string, required ...string) map[string]any {
	p := map[string]any{}
	for k, d := range props {
		t := "string"
		if k == "page" || k == "limit" {
			t = "integer"
		}
		p[k] = map[string]any{"type": t, "description": d}
	}
	return map[string]any{"type": "object", "properties": p, "required": required, "additionalProperties": false}
}

type args struct {
	Query        string `json:"query"`
	Page         int    `json:"page"`
	Conversation string `json:"conversation"`
	Thread       string `json:"thread"`
	Limit        int    `json:"limit"`
	Person       string `json:"person"`
	Text         string `json:"text"`
}

func call(ctx context.Context, name string, raw jsontext.Value) (string, error) {
	var in args
	if len(raw) > 0 {
		if err := jsonx.Unmarshal(raw, &in); err != nil {
			return "", fmt.Errorf("bad arguments: %v", err)
		}
	}
	w, err := open(ctx)
	if err != nil {
		return "", err
	}
	switch name {
	case "slack_search":
		return w.search(ctx, in)
	case "slack_read":
		return w.read(ctx, in)
	case "slack_unread":
		return w.unread(ctx)
	case "slack_who":
		return w.who(ctx, in)
	case "slack_draft":
		return w.draft(ctx, in)
	}
	return "", fmt.Errorf("no tool named %s", name)
}

// --- the workspace ---

type workspace struct {
	team string
	api  *slack.Client
	st   *store.Store
	at   time.Time
}

var (
	mu sync.Mutex
	ws *workspace
)

// open is loafer's default workspace, signed in with loafer's own sign-in
// and booted: who's who and the conversations you're in. It boots again
// once it's half an hour old.
func open(ctx context.Context) (*workspace, error) {
	mu.Lock()
	defer mu.Unlock()
	if ws != nil && time.Since(ws.at) < 30*time.Minute {
		return ws, nil
	}
	team, err := defaultTeam()
	if err != nil {
		return nil, err
	}
	creds, err := slack.Load(team)
	if err != nil {
		return nil, fmt.Errorf("couldn't read loafer's sign-in from the Keychain: %v", err)
	}
	w := &workspace{team: team, api: slack.New(creds), st: store.New(), at: time.Now()}
	b, err := w.api.UserBoot(ctx)
	if err == nil {
		w.st.ApplyBoot(b)
		var n slack.Counts
		if n, err = w.api.Counts(ctx); err == nil {
			w.st.ApplyCounts(n)
		}
	}
	if slack.SignedOut(err) {
		return nil, errors.New("slack has signed loafer out; the user signs in again by running loafer")
	}
	if err != nil {
		return nil, fmt.Errorf("couldn't reach slack: %v", err)
	}
	if err := w.api.Users(ctx, w.st.ApplyPeople); err != nil {
		slog.Warn("users", "err", err) // names show as ids until the next boot
	}
	ws = w
	return w, nil
}

// defaultTeam is the first of loafer's workspaces, from the list the
// manifest lets it read: the sandbox's HOME isn't yours.
func defaultTeam() (string, error) {
	b, err := os.ReadFile(os.Getenv("LOAFER_WORKSPACES"))
	if err != nil {
		return "", fmt.Errorf("couldn't read loafer's workspaces (%v); is LOAFER_WORKSPACES in plugin.json right, and has the user run loafer login?", err)
	}
	var list []slack.Workspace
	if err := jsonx.Unmarshal(b, &list); err != nil || len(list) == 0 {
		return "", errors.New("loafer has no workspace signed in; the user runs loafer login")
	}
	return list[0].TeamID, nil
}

var teamRE = regexp.MustCompile(`^T[A-Z0-9]+$`)

type execOut struct {
	Code   int    `json:"code"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// secret is slack.Secret in the sandbox: rush runs security(1) for it,
// as the manifest's "keychain", which names loafer's service itself.
func secret(service, account string) ([]byte, error) {
	if service != "loafer" || !teamRE.MatchString(account) {
		return nil, errors.New("not loafer's item")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	var out execOut
	if err := rush.Call(ctx, "exec", map[string]any{"name": "keychain", "args": []string{"-a", account}}, &out); err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, errors.New("no such item, or the Keychain said no")
	}
	b := bytes.TrimRight([]byte(out.Stdout), "\n")
	// security prints a secret it doesn't take for text as hex.
	if h, err := hex.DecodeString(string(b)); err == nil && jsonx.Valid(h) {
		return h, nil
	}
	return b, nil
}

// --- finding things ---

var (
	linkRE   = regexp.MustCompile(`/archives/([A-Z0-9]+)(?:/p(\d+)(\d{6}))?`)
	convIDRE = regexp.MustCompile(`^[CDG][A-Z0-9]{2,}$`)
	userIDRE = regexp.MustCompile(`^[UW][A-Z0-9]{2,}$`)
)

// place is the conversation q names, and the thread if a link names one.
func (w *workspace) place(q string) (conv, ts string, err error) {
	q = strings.TrimSpace(q)
	if m := linkRE.FindStringSubmatch(q); m != nil {
		if m[2] != "" {
			ts = m[2] + "." + m[3]
		}
		if u, err := url.Parse(q); err == nil && u.Query().Get("thread_ts") != "" {
			ts = u.Query().Get("thread_ts")
		}
		return m[1], ts, nil
	}
	if convIDRE.MatchString(q) {
		return q, "", nil
	}
	name := strings.ToLower(strings.TrimPrefix(q, "#"))
	if !strings.HasPrefix(q, "@") {
		w.st.Read(func(v store.View) {
			v.EachConv(func(c *store.Conv) {
				if c.Kind != store.IM && strings.ToLower(c.Name) == name {
					conv = c.ID
				}
			})
		})
		if conv != "" || strings.HasPrefix(q, "#") {
			if conv == "" {
				err = fmt.Errorf("no channel named %s among those the user is in", q)
			}
			return conv, "", err
		}
	}
	p, err := w.person(q)
	if err != nil {
		return "", "", err
	}
	w.st.Read(func(v store.View) {
		v.EachConv(func(c *store.Conv) {
			if c.Kind == store.IM && c.User == p.ID {
				conv = c.ID
			}
		})
	})
	if conv == "" {
		return "", "", fmt.Errorf("the user has no DM with %s", p.Name)
	}
	return conv, "", nil
}

// thread is the ts a thread argument names: a ts, or a link's.
func thread(q string) string {
	if _, ts, ok := strings.Cut(q, "thread_ts="); ok {
		ts, _, _ = strings.Cut(ts, "&")
		return ts
	}
	if m := linkRE.FindStringSubmatch(q); m != nil && m[2] != "" {
		return m[2] + "." + m[3]
	}
	return strings.TrimSpace(q)
}

// person is who q names: an id, a handle, or a name or part of one.
func (w *workspace) person(q string) (store.Person, error) {
	q = strings.Trim(strings.TrimSpace(q), "<>@")
	var exact, part []store.Person
	w.st.Read(func(v store.View) {
		if userIDRE.MatchString(q) {
			exact = append(exact, v.Person(q))
			return
		}
		l := strings.ToLower(q)
		v.EachPerson(func(p *store.Person) {
			h, n := strings.ToLower(p.Handle), strings.ToLower(p.Name)
			switch {
			case p.Deleted:
			case h == l || n == l:
				exact = append(exact, *p)
			case strings.Contains(h, l) || strings.Contains(n, l):
				part = append(part, *p)
			}
		})
	})
	if len(exact) == 0 {
		exact = part
	}
	if len(exact) == 0 || q == "" {
		return store.Person{}, fmt.Errorf("no one in the workspace goes by %q", q)
	}
	if len(exact) > 1 {
		slices.SortFunc(exact, func(a, b store.Person) int { return cmp.Compare(a.Name, b.Name) })
		var names []string
		for _, p := range exact[:min(len(exact), 10)] {
			names = append(names, p.Name+" (@"+p.Handle+")")
		}
		return store.Person{}, fmt.Errorf("%d people go by %q: %s; say which", len(exact), q, strings.Join(names, ", "))
	}
	return exact[0], nil
}

// label is how a conversation is named in an answer: the attribute the
// claude pane gives its <slack> element.
func label(v store.View, id string) [2]string {
	c := v.Conv(id)
	switch {
	case c == nil:
		return [2]string{"channel", id}
	case c.Kind == store.Channel || c.Kind == store.Private:
		return [2]string{"channel", "#" + c.Name}
	}
	return [2]string{"dm", v.Title(c)}
}

// --- the tools ---

func (w *workspace) search(ctx context.Context, in args) (string, error) {
	if strings.TrimSpace(in.Query) == "" {
		return "", errors.New("query is required")
	}
	f, err := w.api.Search(ctx, in.Query, max(1, in.Page))
	if err != nil {
		return "", err
	}
	if len(f.Matches) == 0 {
		return "no matches for " + strconv.Quote(in.Query), nil
	}
	marks := strings.NewReplacer("", "", "", "")
	msgs := make([]slack.Message, len(f.Matches))
	for i, m := range f.Matches {
		msgs[i] = slack.Message{TS: m.TS, User: m.User, Username: m.Username, Text: marks.Replace(m.Text)}
	}
	var lines []string
	w.st.Read(func(v store.View) {
		lines = ui.SlackLines(v, msgs)
		for i, m := range f.Matches {
			where := "#" + m.Channel.Name
			if m.Channel.IsIM {
				where = "dm with " + v.Person(m.Channel.Name).Name
			} else if m.Channel.IsMPIM {
				where = "group dm"
			}
			lines[i] = where + " " + lines[i] + "\n  " + m.Permalink
		}
	})
	// shortcut: fit drops from the top, the best hits; fine at 20 a page.
	el := ui.SlackElement([][2]string{{"search", in.Query}, {"page", fmt.Sprintf("%d of %d", f.Page, max(f.Pages, 1))}, {"total", strconv.Itoa(f.Total)}}, lines, most)
	return ui.Untrusted + el, nil
}

func (w *workspace) read(ctx context.Context, in args) (string, error) {
	conv, ts, err := w.place(in.Conversation)
	if err != nil {
		return "", err
	}
	if in.Thread != "" {
		ts = thread(in.Thread)
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 200)
	var msgs []slack.Message
	if ts != "" {
		if msgs, err = w.api.Replies(ctx, conv, ts); err == nil && len(msgs) > limit {
			msgs = append(msgs[:1], msgs[len(msgs)-limit+1:]...) // the parent, then the latest
		}
	} else if msgs, _, err = w.api.History(ctx, conv, "", limit); err == nil {
		slices.Reverse(msgs)
	}
	if err != nil {
		return "", err
	}
	var out string
	w.st.Read(func(v store.View) {
		attrs := [][2]string{label(v, conv)}
		if ts != "" {
			attrs = append(attrs, [2]string{"thread", ts})
		}
		out = ui.Untrusted + ui.SlackElement(attrs, ui.SlackLines(v, msgs), most)
	})
	return out, nil
}

func (w *workspace) unread(ctx context.Context) (string, error) {
	n, err := w.api.Counts(ctx)
	if err != nil {
		return "", err
	}
	w.st.ApplyCounts(n)
	type row struct {
		line     string
		mentions int
		dm       bool
	}
	var rows []row
	w.st.Read(func(v store.View) {
		v.EachConv(func(c *store.Conv) {
			if !c.Unread && c.Mentions == 0 {
				return
			}
			l := label(v, c.ID)
			line := l[1]
			if l[0] == "dm" {
				line += " (dm)"
			}
			if c.Mentions > 0 {
				line += fmt.Sprintf(": %d mentioning the user", c.Mentions)
			} else {
				line += ": unread"
			}
			rows = append(rows, row{line + " [" + c.ID + "]", c.Mentions, l[0] == "dm"})
		})
	})
	rank := func(r row) int { // mentions, then DMs, then the rest
		switch {
		case r.mentions > 0:
			return 0
		case r.dm:
			return 1
		}
		return 2
	}
	slices.SortFunc(rows, func(a, b row) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(b.mentions, a.mentions), cmp.Compare(a.line, b.line))
	})
	lines := make([]string, 0, len(rows)+1)
	if t := n.Threads; t.MentionCount > 0 || t.HasUnreads {
		lines = append(lines, fmt.Sprintf("threads: %d mentioning the user, unread replies: %v", t.MentionCount, t.HasUnreads))
	}
	for _, r := range rows {
		lines = append(lines, r.line)
	}
	if len(lines) == 0 {
		return "nothing unread", nil
	}
	return ui.Untrusted + ui.SlackElement([][2]string{{"unread", strconv.Itoa(len(rows)) + " conversations"}}, lines, most) +
		"slack_read reads any of them. nothing has been marked read.", nil
}

func (w *workspace) who(ctx context.Context, in args) (string, error) {
	p, err := w.person(in.Person)
	if err != nil {
		return "", err
	}
	var r struct {
		User struct {
			TZ      string `json:"tz"`
			Profile struct {
				Title       string `json:"title"`
				StatusText  string `json:"status_text"`
				StatusEmoji string `json:"status_emoji"`
			} `json:"profile"`
		} `json:"user"`
	}
	if err := w.api.Call(ctx, "users.info", url.Values{"user": {p.ID}}, &r); err != nil {
		slog.Warn("users.info", "err", err) // what users.list said is enough
	}
	lines := []string{"name: " + p.Name, "handle: @" + p.Handle, "id: " + p.ID}
	u := r.User
	if u.Profile.Title != "" {
		lines = append(lines, "title: "+u.Profile.Title)
	}
	if loc, err := time.LoadLocation(u.TZ); err == nil && u.TZ != "" {
		lines = append(lines, "timezone: "+u.TZ+", where it's "+time.Now().In(loc).Format("Mon 15:04"))
	}
	if s := strings.TrimSpace(u.Profile.StatusEmoji + " " + u.Profile.StatusText); s != "" {
		lines = append(lines, "status: "+s)
	}
	if p.Bot {
		lines = append(lines, "a bot")
	}
	if p.Deleted {
		lines = append(lines, "deactivated")
	}
	w.st.Read(func(v store.View) {
		v.EachConv(func(c *store.Conv) {
			if c.Kind == store.IM && c.User == p.ID {
				lines = append(lines, "dm: "+c.ID)
			}
		})
	})
	return ui.Untrusted + ui.SlackElement([][2]string{{"person", "@" + p.Handle}}, lines, most), nil
}

func (w *workspace) draft(ctx context.Context, in args) (string, error) {
	text := strings.TrimSpace(in.Text)
	switch {
	case text == "":
		return "", errors.New("text is required")
	case len(text) > maxDraft:
		return "", errors.New("that's longer than one slack message may be")
	}
	conv, ts, err := w.place(in.Conversation)
	if err != nil {
		return "", err
	}
	if in.Thread != "" {
		ts = thread(in.Thread)
	}
	a := []string{w.team, conv}
	if ts != "" {
		a = append(a, ts)
	}
	var out execOut
	if err := rush.Call(ctx, "exec", map[string]any{"name": "loafer", "args": a, "stdin": text}, &out); err != nil {
		return "", fmt.Errorf("couldn't run loafer draft: %v", err)
	}
	var where string
	w.st.Read(func(v store.View) { where = label(v, conv)[1] })
	if ts != "" {
		where = "a thread in " + where
	}
	switch out.Code {
	case 0:
		return "the draft is in loafer's composer for " + where + ". nothing was sent: the user reads it and sends it, or doesn't.", nil
	case 3:
		// shortcut: Slack's drafts.create exists, but its arguments aren't
		// confirmed (docs/slack-webapp-methods.md); use it once they are.
		return "", errors.New("loafer isn't running, so the draft is nowhere yet, and nothing was sent. " +
			"slack's own drafts aren't used: their api isn't confirmed. give the user the text to paste into " + where + ":\n\n" + text)
	}
	return "", fmt.Errorf("loafer didn't take the draft: %s", strings.TrimSpace(cmp.Or(out.Stderr, out.Stdout)))
}
