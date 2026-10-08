package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
)

// fixture is a small workspace with #dev open and some history in it.
func fixture(t testing.TB) *Model {
	st := store.New()
	var b slack.UserBoot
	b.Self.ID = "U0"
	b.Team.Name = "Big Bong"
	b.Channels = []slack.Conversation{
		{ID: "C1", Name: "dev", IsChannel: true, IsMember: true},
		{ID: "C2", Name: "alerts", IsChannel: true, IsMember: true},
	}
	b.IMs = []slack.Conversation{{ID: "D1", User: "U1", IsIM: true}}
	st.ApplyBoot(b)
	st.ApplyPeople([]slack.User{{ID: "U0", Name: "alex"}, {ID: "U1", Name: "drew"}})
	base := time.Now().Add(-time.Hour).Unix()
	var msgs []slack.Message // newest first, as Slack sends them
	for i := range 40 {
		user := "U1"
		if i%3 == 0 {
			user = "U0"
		}
		msgs = append([]slack.Message{{
			TS: fmt.Sprintf("%d.%06d", base+int64(i*60), i), User: user,
			Text: fmt.Sprintf("message %d with *bold* and `code` and a link <https://example.com|example>", i),
		}}, msgs...)
	}
	msgs[0].Reactions = []slack.Reaction{{Name: "tada", Count: 2, Users: []string{"U0", "U1"}}}
	st.SetWindow("C1", msgs, false, false)

	m := New(context.Background(), st, nil)
	m.w, m.h = 120, 40
	m.live = "live"
	m.render() // builds the sidebar
	m.openConv("C1")
	return m
}

func plainFrame(rows []canvas.Row) []string {
	var b strings.Builder
	canvas.Emit(&b, rows)
	return strings.Split(ansi.Strip(b.String()), "\n")
}

func TestFrame(t *testing.T) {
	m := fixture(t)
	for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 10}} {
		m.w, m.h = size[0], size[1]
		rows := m.render()
		if len(rows) != m.h {
			t.Fatalf("%v: %d rows", size, len(rows))
		}
		for i, r := range rows {
			if r.Width() != m.w {
				t.Fatalf("%v: row %d is %d wide: %q", size, i, r.Width(), plainFrame([]canvas.Row{r}))
			}
		}
	}
	m.w, m.h = 120, 40
	text := strings.Join(plainFrame(m.render()), "\n")
	if testing.Verbose() {
		t.Log("\n" + text)
	}
	for _, want := range []string{"Big Bong", "# dev", "drew", "message 39", "╭", "❯"} {
		if !strings.Contains(text, want) {
			t.Errorf("frame lacks %q:\n%s", want, text)
		}
	}
}

func BenchmarkFrame(b *testing.B) {
	m := fixture(b)
	m.render()
	for b.Loop() {
		m.View() // nothing said Keep, so every View draws
	}
}

// A long conversation draws only what's on screen, at either end.
func TestLongWindow(t *testing.T) {
	m := fixture(t)
	var msgs []slack.Message
	base := time.Now().Add(-48 * time.Hour).Unix()
	for i := 2999; i >= 0; i-- { // newest first, as Slack sends them
		msgs = append(msgs, slack.Message{TS: fmt.Sprintf("%d.%06d", base+int64(i*30), i), User: []string{"U0", "U1"}[i%2], Text: fmt.Sprintf("long %d", i)})
	}
	m.st.SetWindow("C1", msgs, false, false)
	m.render()
	if n := m.drawn.Len(); n > 40 {
		t.Fatalf("drew %d messages for one screen at the newest", n)
	}
	press(m, tab, r('g'))
	text := strings.Join(plainFrame(m.render()), "\n")
	if !strings.Contains(text, "long 0\n") && !strings.Contains(text, "long 0 ") {
		t.Fatalf("g should show the oldest:\n%s", text)
	}
	if n := m.drawn.Len(); n > 80 {
		t.Fatalf("drew %d messages to reach the oldest", n)
	}
}

func BenchmarkOldest(b *testing.B) {
	m := fixture(b)
	var msgs []slack.Message
	for i := 2999; i >= 0; i-- {
		msgs = append(msgs, slack.Message{TS: fmt.Sprintf("%d.%06d", 1700000000+i*30, i), User: "U1", Text: fmt.Sprintf("long %d", i)})
	}
	m.st.SetWindow("C1", msgs, false, false)
	m.render()
	press(m, tab)
	for b.Loop() {
		press(m, r('g'), r('G'))
	}
}
