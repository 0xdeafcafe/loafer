package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/jsonx"
)

var ctrlF = tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl}

// alertTS is the ts of #alerts' message i, in the fake Slack below.
func alertTS(i int) string { return fmt.Sprintf("%d.000000", 1600000000+i*60) }

// searchSlack is a fake Slack: a search that finds a reply in #dev's
// thread at parent, and an old message in #alerts, which holds 120.
func searchSlack(t *testing.T, m *Model, parent string) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		var out any
		switch r.URL.Path {
		case "/api/search.messages":
			if q := r.Form.Get("query"); q != "deploy in:<#C1>" && q != "deploy" {
				t.Errorf("searched %q", q)
			}
			out = map[string]any{"ok": true, "messages": map[string]any{
				"total": 2, "paging": map[string]int{"page": 1, "pages": 1},
				"matches": []map[string]any{
					{"channel": map[string]string{"id": "C1", "name": "dev"}, "user": "U1", "ts": "9.0", "text": "the \ue000deploy\ue001 is out",
						"permalink": "https://x.slack.com/archives/C1/p90?thread_ts=" + parent},
					{"channel": map[string]string{"id": "C2", "name": "alerts"}, "user": "U1", "ts": alertTS(10), "text": "Deploy failed"},
				},
			}}
		case "/api/conversations.history":
			latest, oldest := r.Form.Get("latest"), r.Form.Get("oldest")
			limit, _ := strconv.Atoi(r.Form.Get("limit"))
			var page []slack.Message
			more := false
			for i := 119; i >= 0; i-- {
				ts := alertTS(i)
				if (latest != "" && ts > latest) || (oldest != "" && ts < oldest) {
					continue
				}
				if len(page) == limit {
					more = true
					break
				}
				page = append(page, slack.Message{TS: ts, User: "U1", Text: "alert " + strconv.Itoa(i)})
			}
			out = map[string]any{"ok": true, "messages": page, "has_more": more}
		}
		b, _ := jsonx.Marshal(out)
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	m.api = slack.New(slack.Creds{URL: srv.URL, Token: "xoxc-t"})
}

// settle stands in for the wait: the last tick fires, and the search's
// answer comes back.
func settle(m *Model) {
	if cmd := m.searched(searchTickMsg{m.find.gen}); cmd != nil {
		m.searched(cmd())
	}
	m.render()
}

func TestSearch(t *testing.T) {
	m := fixture(t)
	var parent string
	m.st.Read(func(v store.View) { parent = v.Window("C1").Msgs[5].TS })
	searchSlack(t, m, parent)

	press(m, ctrlF)
	if !m.find.on || m.find.here != "C1" {
		t.Fatalf("ctrl+f from #dev: on %v here %q", m.find.on, m.find.here)
	}
	for _, c := range "deploy" {
		press(m, r(c))
	}
	settle(m)
	if len(m.find.found) != 2 || m.find.sent != "deploy in:<#C1>" {
		t.Fatalf("found %d for %q", len(m.find.found), m.find.sent)
	}
	text := strings.Join(plainFrame(m.render()), "\n")
	if testing.Verbose() {
		t.Log("\n" + text)
	}
	for _, want := range []string{"Search messages", "2 results", "⌕ in # dev", "# alerts", "drew", "↩ in a thread", "the deploy is out", "Deploy failed"} {
		if !strings.Contains(text, want) {
			t.Errorf("frame lacks %q:\n%s", want, text)
		}
	}
	press(m, tab) // everywhere now
	settle(m)
	if m.find.here != "" || m.find.sent != "deploy" {
		t.Fatalf("tab: here %q sent %q", m.find.here, m.find.sent)
	}

	// A reply lands on its parent, which #dev holds: at once.
	press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.find.on || m.focus != onMsgs || m.sel != parent {
		t.Fatalf("enter on a reply: on %v focus %v sel %q", m.find.on, m.focus, m.sel)
	}

	// Closed and opened again, it's as it was.
	press(m, r('/'))
	if !m.find.on || len(m.find.found) != 2 || string(m.find.query) != "deploy" {
		t.Fatalf("reopened: %d found for %q", len(m.find.found), string(m.find.query))
	}
	// #alerts isn't held: it's opened, then fetched around the message.
	press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	cmd := m.searchKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.open != "C2" || cmd == nil {
		t.Fatalf("enter on #alerts: open %q", m.open)
	}
	m.searched(cmd())
	m.render()
	if m.sel != alertTS(10) || m.focus != onMsgs {
		t.Fatalf("landed on %q, want %q", m.sel, alertTS(10))
	}
	if text := strings.Join(plainFrame(m.render()), "\n"); !strings.Contains(text, "alert 10") {
		t.Fatalf("the message isn't in view:\n%s", text)
	}
	// Going past the newest held goes back to the newest.
	if cmd := m.pick(newest); cmd != nil {
		m.Update(cmd())
	}
	m.st.Read(func(v store.View) {
		if w := v.Window("C2"); w.Newer || !v.Holds("C2", alertTS(119)) {
			t.Fatalf("not back at the newest: newer %v", w.Newer)
		}
	})
}

func TestSnippet(t *testing.T) {
	base, lit := canvas.Style{}, canvas.Style{}.With(canvas.Bold)
	lits := func(rows []canvas.Row) (out []string) {
		for _, r := range rows {
			for _, s := range r {
				if s.St == lit {
					out = append(out, s.Text)
				}
			}
		}
		return out
	}
	// Slack's marks, when it sent them.
	if got := lits(snippet("a \ue000Deploy\ue001 b", nil, 40, 2, base, lit)); len(got) != 1 || got[0] != "Deploy" {
		t.Errorf("marked: %q", got)
	}
	// Else the query's words, any case, but not its modifiers.
	words := terms(`deploy in:#dev from:@drew -not "out"`)
	if strings.Join(words, ",") != "deploy,out" {
		t.Fatalf("terms: %q", words)
	}
	if got := lits(snippet("DEPLOY is\nout", words, 40, 2, base, lit)); strings.Join(got, ",") != "DEPLOY,out" {
		t.Errorf("unmarked: %q", got)
	}
	// A long lead is cut so the match shows, and it wraps to two rows at most.
	rows := snippet(strings.Repeat("word ", 40)+"deploy "+strings.Repeat("tail ", 40), words, 30, 2, base, lit)
	if len(rows) != 2 || len(lits(rows)) != 1 || !strings.HasPrefix(rows[0][0].Text, "…") {
		t.Errorf("long: %d rows, %q", len(rows), plainFrame(rows))
	}
}
