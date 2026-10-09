package notify

import (
	"strings"
	"testing"
	"time"
)

func prefs(t *testing.T) *Prefs {
	t.Helper()
	var p Prefs
	p.Load([]byte(`{
		"muted_channels": "C9, C8",
		"highlight_words": "Deploy, rollback",
		"at_channel_suppressed_channels": "C7",
		"all_notifications_prefs": "{\"global\":{\"global_desktop\":\"mentions\"},\"channels\":{\"C5\":{\"desktop\":\"everything\"},\"C6\":{\"desktop\":\"nothing\"},\"C4\":{\"muted\":true}}}"
	}`), []byte(`{"dnd_enabled":false}`))
	return &p
}

func TestWants(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	in := func(f func(*Input)) Input {
		i := Input{Conv: "C1", Self: "UME", User: "UA", Text: "hello"}
		f(&i)
		return i
	}
	for _, c := range []struct {
		name string
		in   Input
		want bool
	}{
		{"plain channel message", in(func(*Input) {}), false},
		{"dm", in(func(i *Input) { i.Direct = true }), true},
		{"mention", in(func(i *Input) { i.Text = "hi <@UME>" }), true},
		{"mention with label", in(func(i *Input) { i.Text = "hi <@UME|alex>" }), true},
		{"someone else", in(func(i *Input) { i.Text = "hi <@UMEG>" }), false},
		{"here", in(func(i *Input) { i.Text = "<!here> lunch" }), true},
		{"here where it's suppressed", in(func(i *Input) { i.Conv, i.Text = "C7", "<!here> lunch" }), false},
		{"keyword", in(func(i *Input) { i.Text = "the DEPLOY is out" }), true},
		{"keyword inside a word", in(func(i *Input) { i.Text = "deployment" }), false},
		{"thread you follow", in(func(i *Input) { i.Reply, i.Following = true, true }), true},
		{"thread you don't", in(func(i *Input) { i.Reply = true }), false},
		{"yours", in(func(i *Input) { i.User, i.Direct = "UME", true }), false},
		{"looking at it", in(func(i *Input) { i.Direct, i.Looking = true, true }), false},
		{"muted in prefs", in(func(i *Input) { i.Conv, i.Direct = "C9", true }), false},
		{"muted in all_notifications_prefs", in(func(i *Input) { i.Conv, i.Text = "C4", "<@UME>" }), false},
		{"everything for this one", in(func(i *Input) { i.Conv = "C5" }), true},
		{"nothing for this one", in(func(i *Input) { i.Conv, i.Text = "C6", "<@UME>" }), false},
		{"a join", in(func(i *Input) { i.Direct, i.Subtype = true, "channel_join" }), false},
		{"a bot", in(func(i *Input) { i.Direct, i.Subtype = true, "bot_message" }), true},
		{"before boot", in(func(i *Input) { i.Self, i.Direct = "", true }), false},
	} {
		if got := prefs(t).Wants(c.in, now); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDND(t *testing.T) {
	var p Prefs
	now := time.Unix(1000, 0)
	p.SetDND([]byte(`{"dnd_enabled":true,"next_dnd_start_ts":900,"next_dnd_end_ts":1100}`))
	if !p.Quiet(now) || p.Quiet(time.Unix(1100, 0)) || p.Quiet(time.Unix(800, 0)) {
		t.Fatal("dnd should hold between its start and end only")
	}
	p.SetDND([]byte(`{"dnd_enabled":false,"snooze_enabled":true,"snooze_endtime":1500}`))
	if !p.Quiet(now) || p.Quiet(time.Unix(1500, 0)) {
		t.Fatal("a snooze should hold until its end")
	}
	if p.Wants(Input{Conv: "D1", Self: "UME", User: "UA", Direct: true}, now) {
		t.Fatal("nothing should notify in dnd")
	}
}

func TestPlain(t *testing.T) {
	got := Plain("hey <@U1> in <#C1|dev>: *look* at <https://x.dev|this> &amp; `that`\u001b[2J :tada:", func(id string) string { return "Drew" })
	if want := "hey @Drew in #dev: look at this & that [2J :tada:"; got != want {
		t.Fatalf("%q, want %q", got, want)
	}
	if long := Plain(strings.Repeat("a", 500), nil); len([]rune(long)) != 200 {
		t.Fatalf("a long message should be cut: %d", len([]rune(long)))
	}
}

func TestEscape(t *testing.T) {
	n := Note{Title: "#dev;", Body: "hi\x07\x1b]0;pwned\x07 there"}
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if got := Escape(env(map[string]string{"TERM_PROGRAM": "ghostty"}), n); got != "\x1b]777;notify;#dev,;hi ]0;pwned there\x07" {
		t.Fatalf("ghostty: %q", got)
	}
	if got := Escape(env(map[string]string{"TERM_PROGRAM": "iTerm.app"}), n); !strings.HasPrefix(got, "\x1b]9;") || strings.Count(got, "\x1b") != 1 {
		t.Fatalf("iterm: %q", got)
	}
	if Escape(env(map[string]string{"TERM_PROGRAM": "iTerm.app", "TMUX": "x"}), n) != "" || Escape(env(nil), n) != "" {
		t.Fatal("no escape under tmux or in a terminal we don't know")
	}
}

func TestThrottle(t *testing.T) {
	var th Throttle
	t0 := time.Unix(100, 0)
	if _, ok := th.Push(Note{Title: "#a"}, t0); !ok {
		t.Fatal("the first should show")
	}
	for _, title := range []string{"#a", "#b", "#a"} {
		if _, ok := th.Push(Note{Title: title}, t0.Add(time.Second)); ok {
			t.Fatal("a burst should be held")
		}
	}
	if w := th.Wait(t0.Add(time.Second)); w != Gap-time.Second {
		t.Fatalf("wait %v", w)
	}
	n, ok := th.Flush(t0.Add(Gap))
	if !ok || n.Title != "3 new messages" || n.Body != "#a, #b" {
		t.Fatalf("flush: %+v", n)
	}
	if _, ok := th.Flush(t0); ok || th.Wait(t0) != 0 {
		t.Fatal("nothing should be left held")
	}
	if _, ok := th.Push(Note{}, t0.Add(Gap+time.Second)); ok {
		t.Fatal("a flush counts as showing")
	}
}
