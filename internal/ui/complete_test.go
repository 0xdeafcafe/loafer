package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
)

var (
	cEnter     = tea.KeyPressMsg{Code: tea.KeyEnter}
	cBackspace = tea.KeyPressMsg{Code: tea.KeyBackspace}
	cDown      = tea.KeyPressMsg{Code: tea.KeyDown}
	cCtrlU     = tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	cCtrlLeft  = tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl}
	cNewline   = tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}
)

// composer is the fixture with the composer focused and a few more people.
func composeFixture(t testing.TB) *Model {
	m := fixture(t)
	m.st.ApplyPeople([]slack.User{
		{ID: "U2", Name: "alexa", RealName: "Alexa Fox"},
		{ID: "U3", Name: "alertbot", IsBot: true},
		{ID: "U4", Name: "alfred", Deleted: true},
	})
	press(m, tab, tab)
	return m
}

func typeText(m *Model, s string) {
	for _, c := range s {
		press(m, r(c))
	}
}

func popLabels(m *Model) []string {
	var out []string
	for _, it := range m.pop.items {
		out = append(out, it.label)
	}
	return out
}

func TestMentionPeople(t *testing.T) {
	m := composeFixture(t)
	typeText(m, "hi @al")
	if got := strings.Join(popLabels(m), " "); !m.pop.on || !strings.Contains(got, "@alex") || !strings.Contains(got, "@Alexa Fox") || strings.Contains(got, "alfred") {
		t.Fatalf("popup %v: %v", m.pop.on, got)
	}
	if text := strings.Join(plainFrame(m.render()), "\n"); !strings.Contains(text, "tab accept") {
		t.Fatalf("no popup drawn:\n%s", text)
	}
	press(m, cDown)
	for m.pop.items[m.pop.at].label != "@alex" {
		press(m, cDown)
	}
	press(m, cEnter)
	if string(m.input) != "hi @alex " || len(m.ments) != 1 || m.pop.on || m.editing != "" {
		t.Fatalf("input %q ments %v", string(m.input), m.ments)
	}
	if got := encode(m.input, m.ments); got != "hi <@U0> " {
		t.Fatalf("encode %q", got)
	}
	typeText(m, "ok & <3")
	if got := encode(m.input, m.ments); got != "hi <@U0> ok &amp; &lt;3" {
		t.Fatalf("encode %q", got)
	}
	// Backspace into a mention takes all of it.
	press(m, cCtrlU)
	if len(m.input) != 0 || len(m.ments) != 0 {
		t.Fatalf("ctrl+u left %q %v", string(m.input), m.ments)
	}
	typeText(m, "@alexa")
	press(m, cEnter)
	press(m, cBackspace, cBackspace)
	if string(m.input) != "" || len(m.ments) != 0 {
		t.Fatalf("cBackspace left %q %v", string(m.input), m.ments)
	}
}

func TestMentionEscAndSpecials(t *testing.T) {
	m := composeFixture(t)
	typeText(m, "@dr")
	press(m, esc)
	typeText(m, "e")
	if m.pop.on || m.focus != onCompose {
		t.Fatalf("esc should close and keep the popup closed: on %v focus %v", m.pop.on, m.focus)
	}
	press(m, cCtrlU)
	typeText(m, "ping @he")
	if popLabels(m)[0] != "@here" {
		t.Fatalf("labels %v", popLabels(m))
	}
	press(m, tab)
	if got := encode(m.input, m.ments); got != "ping <!here> " {
		t.Fatalf("encode %q", got)
	}
	typeText(m, "a@b") // not after a space: no popup
	if m.pop.on {
		t.Fatal("an email isn't a mention")
	}
}

func TestMentionChannels(t *testing.T) {
	m := composeFixture(t)
	typeText(m, "see #al")
	if len(m.pop.items) != 1 || m.pop.items[0].code != "<#C2>" {
		t.Fatalf("items %+v", m.pop.items)
	}
	press(m, cEnter)
	if got := encode(m.input, m.ments); got != "see <#C2> " || string(m.input) != "see #alerts " {
		t.Fatalf("encode %q box %q", got, string(m.input))
	}
}

func TestMentionsMoveWithEdits(t *testing.T) {
	m := composeFixture(t)
	typeText(m, "@alex")
	press(m, cEnter)
	typeText(m, "end")
	press(m, tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}, r('>'), r('>'))
	if got := encode(m.input, m.ments); got != "&gt;&gt;<@U0> end" {
		t.Fatalf("encode %q", got)
	}
	m.splice(3, 3, []rune("x")) // inside the mention: it's plain text now
	if len(m.ments) != 0 {
		t.Fatalf("ments %v", m.ments)
	}
}

func TestDecodeRoundTrips(t *testing.T) {
	m := fixture(t)
	const in = "hey <@U1> see <#C1|dev> <!here> &lt; <https://x.y|x> <!subteam^S1|@crew> &amp;"
	m.st.Read(func(v store.View) {
		text, ments := decode(v, in)
		if got := string(text); got != "hey @drew see #dev @here < x @crew &" {
			t.Fatalf("box %q", got)
		}
		if len(ments) != 5 {
			t.Fatalf("ments %v", ments)
		}
		if got := encode(text, ments); got != in {
			t.Fatalf("encode %q", got)
		}
	})
}

func TestComposerKeys(t *testing.T) {
	m := composeFixture(t)
	typeText(m, "one two")
	press(m, cNewline)
	typeText(m, "three")
	if string(m.input) != "one two\nthree" {
		t.Fatalf("input %q", string(m.input))
	}
	press(m, up)
	if m.cur != 5 {
		t.Fatalf("up: cur %d", m.cur)
	}
	press(m, cDown)
	if m.cur != 13 {
		t.Fatalf("cDown: cur %d", m.cur)
	}
	press(m, cCtrlLeft)
	if m.cur != 8 {
		t.Fatalf("ctrl+left: cur %d", m.cur)
	}
	press(m, cCtrlU)
	if string(m.input) != "one two\nthree" || m.cur != 8 {
		t.Fatalf("ctrl+u at the start of a line: %q", string(m.input))
	}
	press(m, tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}, cCtrlU)
	if string(m.input) != "one two\n" {
		t.Fatalf("ctrl+u: %q", string(m.input))
	}
}
