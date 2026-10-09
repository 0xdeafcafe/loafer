package ui

import (
	"encoding/json/jsontext"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
)

func TestRemindChoices(t *testing.T) {
	at := func(now string, i int) string {
		n, err := time.ParseInLocation("2006-01-02 15:04 Mon", now, time.UTC)
		if err != nil {
			t.Fatal(err)
		}
		return remindChoices[i].at(n).Format("2006-01-02 15:04 Mon")
	}
	for _, c := range []struct {
		now  string
		i    int
		want string
	}{
		{"2026-10-09 15:00 Fri", 0, "2026-10-09 15:20 Fri"},
		{"2026-10-09 15:00 Fri", 1, "2026-10-09 16:00 Fri"},
		{"2026-10-09 23:30 Fri", 2, "2026-10-10 02:30 Sat"},
		{"2026-10-09 15:00 Fri", 3, "2026-10-10 09:00 Sat"},
		{"2026-10-09 03:00 Fri", 3, "2026-10-10 09:00 Sat"}, // tomorrow, even before 9
		{"2026-10-09 15:00 Fri", 4, "2026-10-12 09:00 Mon"},
		{"2026-10-11 15:00 Sun", 4, "2026-10-12 09:00 Mon"},
		{"2026-10-12 08:00 Mon", 4, "2026-10-19 09:00 Mon"}, // not today
	} {
		if got := at(c.now, c.i); got != c.want {
			t.Errorf("%s, %s: %s, want %s", c.now, remindChoices[c.i].label, got, c.want)
		}
	}
}

func TestTSBefore(t *testing.T) {
	for in, want := range map[string]string{
		"1696789123.000200": "1696789123.000199",
		"1696789123.000000": "1696789122.999999",
	} {
		if got := tsBefore(in); got != want {
			t.Errorf("tsBefore(%s) = %s, want %s", in, got, want)
		}
	}
}

// The menu's keys can't share one, and can't be the cursor's.
func TestMenuKeys(t *testing.T) {
	m := fixture(t)
	press(m, tab, r('g'))
	msg, _ := m.selected()
	msg.User = "U0" // yours, so every item is there
	m.acts.menu = menu{on: true, link: true, conv: "C1", msg: msg}
	var keys []string
	m.st.Read(func(v store.View) {
		for _, it := range m.menuList(v) {
			keys = append(keys, it.key)
		}
	})
	if len(keys) != 14 {
		t.Fatalf("keys %v", keys)
	}
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] || slices.Contains([]string{"j", "k", "up", "down", "enter", "esc", "."}, k) {
			t.Errorf("%q clashes: %v", k, keys)
		}
		seen[k] = true
	}
}

func TestDrawnText(t *testing.T) {
	m := fixture(t)
	msg := slack.Message{
		TS: "1.0", User: "U1",
		Blocks: jsontext.Value(`[
			{"type":"section","text":{"type":"mrkdwn","text":"*Plan:* 1 to add\n` + "```go\\nfunc f() {}\\n```" + `"}},
			{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"from rich text "},{"type":"link","url":"https://example.com/rich"}]}]},
			{"type":"actions","elements":[
				{"type":"button","text":{"type":"plain_text","text":"Review"},"url":"https://example.com/review"},
				{"type":"button","text":{"type":"plain_text","text":"Sneaky"},"url":"javascript:alert(1)"}]}]`),
		Attachments: jsontext.Value(`[{"color":"2eb67d","title":"Staging","title_link":"https://example.com/staging","text":"3 services"}]`),
		Text:        "the fallback, which isn't drawn",
	}
	var text string
	var links []string
	m.st.Read(func(v store.View) {
		rows := m.asDrawn(v, &msg)
		text, links = m.rowsText(rows), webLinks(rows)
	})
	for _, want := range []string{"Plan: 1 to add", "func f() {}", "from rich text", "Review", "Staging", "3 services"} {
		if !strings.Contains(text, want) {
			t.Errorf("the copy lacks %q:\n%s", want, text)
		}
	}
	for _, not := range []string{"fallback", "▌", "  go"} { // the text, the bar, the code's language set at the right
		if strings.Contains(text, not) {
			t.Errorf("the copy has %q:\n%s", not, text)
		}
	}
	for _, l := range strings.Split(text, "\n") {
		if l != strings.TrimRight(l, " ") {
			t.Errorf("trailing space on %q", l)
		}
	}
	want := []string{"https://example.com/rich", "https://example.com/review", "https://example.com/staging"}
	if !slices.Equal(links, want) {
		t.Errorf("links %v, want %v", links, want)
	}
}
