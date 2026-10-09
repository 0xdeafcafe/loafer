package ui

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
)

// addNewest adds a message to #dev and returns its ts.
func addNewest(t *testing.T, m *Model, text string) string {
	ts := fmt.Sprintf("%d.000001", time.Now().Unix())
	m.st.Add("C1", slack.Message{TS: ts, User: "U1", Text: text})
	return ts
}

// reactionsOn is the names of the reactions on a message in #dev, and who gave them.
func reactionsOn(m *Model, ts string) (got map[string][]string) {
	got = map[string][]string{}
	m.st.Read(func(v store.View) {
		for _, msg := range v.Window("C1").Msgs {
			if msg.TS == ts {
				for _, r := range msg.Reactions {
					got[r.Name] = r.Users
				}
			}
		}
	})
	return got
}

var emojiEnter = tea.KeyPressMsg{Code: tea.KeyEnter}

func TestEmojiInText(t *testing.T) {
	m := fixture(t)
	addNewest(t, m, "hello :smile: :nonesuch: :+1::skin-tone-3: :thumbsup:")
	rows := m.render()
	text := strings.Join(plainFrame(rows), "\n")
	if !strings.Contains(text, "hello 😄 :nonesuch: 👍🏼 👍") {
		t.Fatalf("shortcodes not drawn:\n%s", text)
	}
	for i, row := range rows { // wide characters keep the rows aligned
		if row.Width() != m.w {
			t.Fatalf("row %d is %d wide", i, row.Width())
		}
	}
}

func TestReactPicker(t *testing.T) {
	m := fixture(t)
	ts := addNewest(t, m, "react to me")
	press(m, tab, r('r'))
	if !m.emo.pick.on || m.emo.pick.ts != ts {
		t.Fatalf("r should open the picker on the newest message: %+v", m.emo.pick)
	}
	typeText(m, "thumbsu")
	if got := m.emo.pick.items; len(got) == 0 || got[0].name != "thumbsup" {
		t.Fatalf("thumbsu found %+v", got)
	}
	text := strings.Join(plainFrame(m.render()), "\n")
	if testing.Verbose() {
		t.Log("\n" + text)
	}
	if !strings.Contains(text, "☺ React") || !strings.Contains(text, "👍 thumbsup") {
		t.Fatalf("picker not drawn:\n%s", text)
	}
	press(m, emojiEnter)
	if m.emo.pick.on {
		t.Fatal("enter should close the picker")
	}
	// The alias is filed as Slack names it, so its event finds it done.
	if got := reactionsOn(m, ts); !slices.Equal(got["+1"], []string{"U0"}) || len(got) != 1 {
		t.Fatalf("after adding: %v", got)
	}
	if !slices.Equal(m.emo.recent, []string{"+1"}) {
		t.Fatalf("recent: %v", m.emo.recent)
	}

	// Again on one that's yours takes it away; it was marked.
	press(m, r('r'))
	if !slices.Equal(m.emo.pick.mine, []string{"+1"}) {
		t.Fatalf("mine: %v", m.emo.pick.mine)
	}
	if m.emo.pick.items[0].name != "+1" { // what you use comes first
		t.Fatalf("first: %+v", m.emo.pick.items[0])
	}
	press(m, emojiEnter)
	if got := reactionsOn(m, ts); len(got) != 0 {
		t.Fatalf("after removing: %v", got)
	}

	// esc closes it without doing anything.
	press(m, r('r'), esc)
	if m.emo.pick.on || len(reactionsOn(m, ts)) != 0 {
		t.Fatal("esc should close the picker and change nothing")
	}
}

func TestReactNumber(t *testing.T) {
	m := fixture(t)
	var oldest string                                                     // the newest, which fixture gives a tada
	m.st.Read(func(v store.View) { oldest = v.Window("C1").Msgs[39].TS }) // has tada from U0 and U1
	press(m, tab, r('1'))
	if got := reactionsOn(m, oldest)["tada"]; !slices.Equal(got, []string{"U1"}) {
		t.Fatalf("1 should take away yours: %v", got)
	}
	press(m, r('1'))
	if got := reactionsOn(m, oldest)["tada"]; !slices.Equal(got, []string{"U1", "U0"}) {
		t.Fatalf("1 again should put it back: %v", got)
	}
	press(m, r('5'))
	if m.flash == "" {
		t.Fatal("5 with one reaction should say so")
	}
}

func TestEmojiComplete(t *testing.T) {
	m := fixture(t)
	press(m, tab, tab) // the composer
	typeText(m, "see :sm")
	if !m.pop.on || len(m.pop.items) == 0 || m.pop.items[0].label != ":smile:" || m.pop.items[0].detail != "😄" {
		t.Fatalf("popup for :sm: %+v", m.pop)
	}
	text := strings.Join(plainFrame(m.render()), "\n")
	if testing.Verbose() {
		t.Log("\n" + text)
	}
	if !strings.Contains(text, ":smile:  😄") {
		t.Fatalf("popup not drawn:\n%s", text)
	}
	press(m, tab)
	if got := string(m.input); got != "see :smile: " || m.pop.on || m.focus != onCompose {
		t.Fatalf("tab should complete it, got %q (focus %v)", got, m.focus)
	}

	// esc closes it but not the composer.
	typeText(m, ":sm")
	press(m, esc)
	if m.pop.on || m.focus != onCompose {
		t.Fatal("esc should only close the popup")
	}

	// enter completes rather than sends.
	typeText(m, " :tad")
	press(m, emojiEnter)
	if got := string(m.input); got != "see :smile: :sm :tada: " || len(m.drafts) != 0 {
		t.Fatalf("enter should complete, not send: %q", got)
	}

	// Times and smileys aren't shortcodes.
	m.input, m.cur = nil, 0
	typeText(m, "at 10:30 :) x:ab")
	if m.pop.on {
		t.Fatalf("popup for %q", string(m.input))
	}
}
