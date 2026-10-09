package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slacktest"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/jsonx"
)

var (
	ctrlEnter = tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}
	keyDown   = tea.KeyPressMsg{Code: tea.KeyDown}
)

// What presses in a message is its actions' elements and its sections'
// accessories, in the order they're drawn.
func TestTargets(t *testing.T) {
	var got []string
	for _, x := range targets(appMsg.Blocks) {
		got = append(got, x.el.Type+":"+x.el.ActionID)
	}
	if want := "button:inspect button:review button: static_select:pick"; strings.Join(got, " ") != want {
		t.Fatalf("targets: %v", got)
	}
}

// The chosen chip is found by what it looks like and how many like it
// come first, and only that one is lit.
func TestLit(t *testing.T) {
	m := fixture(t)
	seg := canvas.T(" Go ", m.pal.Chip)
	rows := []canvas.Row{{seg, canvas.T("  ", m.pal.Main.Text), seg}, {seg}}
	m.kit = blockKit{ts: "1", seg: seg, nth: 1}
	m.lit("1", rows)
	if rows[0][0].St != seg.St || rows[0][2].St.BG != m.pal.Orange.FG || rows[1][0].St != seg.St {
		t.Fatalf("lit the wrong one: %+v", rows)
	}
}

// Inputs go to views.submit as Slack's state.values.
func TestModalValues(t *testing.T) {
	md := modalUI{fields: []field{
		{block: "b1", action: "a1", kind: "plain_text_input", text: []rune("hi")},
		{block: "b2", action: "a2", kind: "checkboxes", opts: []option{{Value: "x"}, {Value: "y"}}, on: []bool{false, true}},
		{block: "b3", action: "a3", kind: "static_select", opts: []option{{Value: "z"}}, on: []bool{false}},
		{block: "b4", action: "a4", kind: "datepicker", text: []rune("2026-10-09")},
	}}
	b, err := jsonx.Marshal(md.values())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"b1":{"a1":{"type":"plain_text_input","value":"hi"}}`, `"selected_options":[{"text":{"type":"","text":""},"value":"y"}]`,
		`"b3":{"a3":{"selected_option":null,"type":"static_select"}}`, `"selected_date":"2026-10-09"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("lacks %s:\n%s", want, b)
		}
	}
}

// From the message cursor to a button, Slack's view_opened, the modal,
// the app turning a submit down, then taking it; then the overflow's
// chooser, and leaving a modal with something typed in it.
func TestE2EPress(t *testing.T) {
	d := newE2E(t)
	d.jump("deploybot", slacktest.DeployDM)
	d.until("the ask", func() bool { return d.has("Not today") })
	d.press(esc, r('b'), enter)
	d.until("the modal", func() bool { return d.has("Review the deploy") && d.has("a line for the log") && d.has("◉ ship it") })
	last := func(method string) (f map[string]string) {
		for _, c := range d.srv.Calls() {
			if c.Method == method {
				f = map[string]string{}
				for k := range c.Form {
					f[k] = c.Form.Get(k)
				}
			}
		}
		return f
	}
	a := last("blocks.actions")
	if a["service_id"] != "B0DEPLOY" || a["app_id"] != "A0DEPLOY" || !strings.Contains(a["actions"], `"action_id":"review"`) ||
		!strings.Contains(a["container"], slacktest.DeployDM) || !strings.HasPrefix(a["client_token"], "web-") {
		t.Fatalf("blocks.actions: %v", a)
	}

	d.press(tab)
	d.typed("ok")
	d.press(ctrlEnter)
	d.until("the app's no", func() bool { return d.has("! say a little more than that") })
	d.typed(" then, ship it")
	d.press(ctrlEnter)
	d.until("it closed", func() bool { return !d.has("Review the deploy") })
	s := last("views.submit")["state"]
	if !strings.Contains(s, `"why":{"why_text":{"type":"plain_text_input","value":"ok then, ship it"}}`) || !strings.Contains(s, `"value":"ship"`) {
		t.Fatalf("views.submit state: %s", s)
	}

	d.press(r('b'), r('b'), enter)
	d.until("the chooser", func() bool { return d.has("page whoever's on call") })
	d.press(keyDown, enter)
	d.until("the modal again", func() bool { return d.has("Review the deploy") })
	if a := last("blocks.actions")["actions"]; !strings.Contains(a, `"selected_option"`) || !strings.Contains(a, `"value":"page"`) {
		t.Fatalf("overflow: %s", a)
	}
	d.press(tab)
	d.typed("hm")
	d.press(esc)
	if !d.has("leave this form?") {
		t.Fatalf("left without asking:\n%s", d.text)
	}
	d.press(r('n'))
	if d.has("leave this form?") || !d.has("Review the deploy") {
		t.Fatalf("n should keep editing:\n%s", d.text)
	}
	d.press(esc, r('y'))
	if d.has("Review the deploy") {
		t.Fatalf("still open:\n%s", d.text)
	}
	d.until("views.close", func() bool { return last("views.close")["view_id"] == slacktest.Review })
}
