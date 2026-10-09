package ui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
)

func alt(c rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: c, Mod: tea.ModAlt} }

var enter = tea.KeyPressMsg{Code: tea.KeyEnter}

// fetchTab opens tab n and runs the fetch it asks for against a server
// that knows message 10 of #dev.
func fetchTab(t *testing.T, m *Model, n rune, l store.List) {
	t.Helper()
	if m.api == nil {
		var ts string
		m.st.Read(func(v store.View) { ts = v.Window("C1").Msgs[10].TS })
		due := time.Now().Add(-49 * time.Hour).Unix()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/activity.feed":
				fmt.Fprintf(w, `{"ok":true,"items":[{"key":"k1","feed_ts":%q,"is_unread":true,"item":{"type":"at_user","message":{"ts":%q,"channel":"C1","author_user_id":"U1","text":"hey <@U0> look"}}}]}`, ts, ts)
			case "/api/saved.list":
				fmt.Fprintf(w, `{"ok":true,"saved_items":[{"item_type":"message","item_id":"C1","ts":%q,"date_due":%d,"state":"in_progress","message":{"ts":%q,"user":"U1","text":"save me"}}]}`, ts, due, ts)
			case "/api/conversations.history":
				io.WriteString(w, `{"ok":true,"messages":[{"ts":"1.0","user":"U1","text":"latest in the dm"}]}`)
			default:
				io.WriteString(w, `{"ok":true}`)
			}
		}))
		t.Cleanup(srv.Close)
		m.api = slack.New(slack.Creds{URL: srv.URL, Token: "xoxc-t", Cookie: "c"})
	}
	press(m, alt(n))
	if !m.tabs.busy[l] {
		t.Fatalf("alt+%c should fetch", n)
	}
	m.Update(tabMsg{l: l, fetch: true, err: m.st.Fetch(context.Background(), m.api, l)})
	m.render()
}

func TestTabs(t *testing.T) {
	m := fixture(t)
	frame := func() string { return strings.Join(plainFrame(m.render()), "\n") }

	fetchTab(t, m, '3', store.ActivityList)
	text := frame()
	if testing.Verbose() {
		t.Log("\n" + text)
	}
	for _, want := range []string{"ACTIVITY", "Activity ·1", "today", "@ drew", "# dev", "@alex look"} {
		if !strings.Contains(text, want) {
			t.Errorf("activity lacks %q:\n%s", want, text)
		}
	}
	press(m, enter)
	var ts string
	m.st.Read(func(v store.View) {
		ts = v.Window("C1").Msgs[10].TS
		if v.Activity()[0].Unread {
			t.Error("enter should mark it read")
		}
	})
	if m.open != "C1" || m.focus != onMsgs || m.sel != ts {
		t.Fatalf("enter: open %q focus %v sel %q, want %q", m.open, m.focus, m.sel, ts)
	}

	fetchTab(t, m, '4', store.LaterList)
	if testing.Verbose() {
		t.Log("\n" + frame())
	}
	if text := frame(); !strings.Contains(text, "overdue 2d") || !strings.Contains(text, "save me") || !strings.Contains(text, "Later ·1") {
		t.Fatalf("later:\n%s", text)
	}
	press(m, r('d'))
	m.st.Read(func(v store.View) {
		if len(v.Saved()) != 0 {
			t.Fatal("d should take it off the list")
		}
	})

	fetchTab(t, m, '2', store.DMList)
	if text := frame(); !strings.Contains(text, "DIRECT MESSAGES") || !strings.Contains(text, "● drew") || !strings.Contains(text, "╰ latest in the dm") {
		t.Fatalf("dms:\n%s", text)
	}
	press(m, enter)
	if m.open != "D1" || m.focus != onCompose {
		t.Fatalf("enter on a dm: open %q focus %v", m.open, m.focus)
	}

	press(m, alt('5'))
	if text := frame(); !strings.Contains(text, "Claude needs rush") {
		t.Fatalf("claude:\n%s", text)
	}

	for _, n := range "12345" {
		press(m, alt(n))
		for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 10}} {
			m.w, m.h = size[0], size[1]
			rows := m.render()
			if len(rows) != m.h {
				t.Fatalf("tab %c %v: %d rows", n, size, len(rows))
			}
			for i, row := range rows {
				if row.Width() != m.w {
					t.Fatalf("tab %c %v: row %d is %d wide: %q", n, size, i, row.Width(), plainFrame(rows[i:i+1]))
				}
			}
		}
		m.w, m.h = 120, 40
	}
}
