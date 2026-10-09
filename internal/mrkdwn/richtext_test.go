package mrkdwn

import (
	"encoding/json/jsontext"
	"testing"
)

// What Slack's composer sends for a message with most of the trimmings.
const richSample = `[
 {"type":"rich_text_section","elements":[
  {"type":"text","text":"hey "},{"type":"user","user_id":"U0"},{"type":"text","text":", "},
  {"type":"channel","channel_id":"C2"},{"type":"text","text":" is "},{"type":"text","text":"done","style":{"bold":true}},
  {"type":"text","text":" "},{"type":"emoji","name":"tada","unicode":"1f389"},{"type":"text","text":"\nnotes:\n"}]},
 {"type":"rich_text_list","style":"bullet","indent":0,"border":0,"elements":[
  {"type":"rich_text_section","elements":[{"type":"text","text":"api is "},{"type":"text","text":"green","style":{"italic":true}}]},
  {"type":"rich_text_section","elements":[{"type":"link","url":"https://grafana.example.com/d/abc","text":"dashboard"}]}]},
 {"type":"rich_text_list","style":"ordered","indent":1,"offset":2,"border":0,"elements":[
  {"type":"rich_text_section","elements":[{"type":"text","text":"check p99"}]}]},
 {"type":"rich_text_preformatted","border":0,"elements":[{"type":"text","text":"kubectl rollout status deploy/api\nok\n"}]},
 {"type":"rich_text_quote","elements":[{"type":"text","text":"ship it "},{"type":"broadcast","range":"here"}]},
 {"type":"rich_text_section","elements":[
  {"type":"text","text":"old","style":{"strike":true}},{"type":"text","text":" "},{"type":"text","text":"x := 1","style":{"code":true}},
  {"type":"text","text":" "},{"type":"usergroup","usergroup_id":"S0614TZR7"},{"type":"emoji","name":"partyparrot"},
  {"type":"date","timestamp":1720710212,"format":"{date_short}","fallback":"11 jul"}]}
]`

func TestRichText(t *testing.T) {
	lines, ok := RichText(jsontext.Value(richSample))
	if !ok {
		t.Fatal("didn't read")
	}
	if len(lines) != 9 {
		for _, l := range lines {
			t.Logf("%+v", l)
		}
		t.Fatalf("%d lines, want 9", len(lines))
	}
	first := lines[0].Spans
	if first[1] != (Span{Kind: User, Target: "U0"}) || first[3] != (Span{Kind: Channel, Target: "C2"}) ||
		first[5] != (Span{Kind: Text, Mark: Bold, Text: "done"}) || first[7].Text != "🎉" {
		t.Errorf("first line: %+v", first)
	}
	if lines[1].Spans[0].Text != "notes:" || lines[1].Bullet != "" {
		t.Errorf("the section's last newline should only end it: %+v", lines[1])
	}
	if l := lines[2]; l.Bullet != "•" || l.Spans[1].Mark != Italic {
		t.Errorf("bullet: %+v", l)
	}
	if s := lines[3].Spans[0]; s.Kind != Link || s.Target != "https://grafana.example.com/d/abc" || s.Text != "dashboard" {
		t.Errorf("link: %+v", s)
	}
	if l := lines[4]; l.Bullet != "3." || l.Depth != 1 {
		t.Errorf("ordered: %+v", l)
	}
	if l := lines[5]; !l.Pre || !l.Start || l.Spans[0].Text != "kubectl rollout status deploy/api" || !lines[6].Pre || lines[6].Start {
		t.Errorf("preformatted: %+v %+v", l, lines[6])
	}
	if l := lines[7]; !l.Quote || l.Spans[1] != (Span{Kind: Special, Target: "here", Text: "@here"}) {
		t.Errorf("quote: %+v", l)
	}
	last := lines[8].Spans
	if last[0].Mark != Strike || last[2].Kind != Code || last[4].Kind != Group || last[5] != (Span{Kind: Emoji, Text: "partyparrot"}) || last[6].Text != "11 jul" {
		t.Errorf("last line: %+v", last)
	}

	if _, ok := RichText(jsontext.Value(`[{"type":"rich_text_hologram","elements":[]}]`)); ok {
		t.Error("an unknown part should give up, so the text shows instead")
	}
}

func TestPlain(t *testing.T) {
	got := Plain(":rotating_light: CRITICAL: 3 at 10:30 *not bold*\nnext")
	want := []Span{{Kind: Emoji, Text: "rotating_light"}, {Text: " CRITICAL: 3 at 10:30 *not bold*"}}
	if len(got) != 2 || len(got[0].Spans) != 2 || got[0].Spans[0] != want[0] || got[0].Spans[1] != want[1] || got[1].Spans[0].Text != "next" {
		t.Fatalf("got %+v", got)
	}
}
