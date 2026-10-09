package ui

import (
	"encoding/json/jsontext"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
)

// A person's message: the composer's rich_text, with text that says the same.
var humanMsg = slack.Message{
	TS: "1760000000.000100", User: "U1",
	Text: "hey <@U0>, <#C2> is *done* :tada:\n• api ok\n```make deploy```",
	Blocks: jsontext.Value(`[{"type":"rich_text","block_id":"Xk2Fh","elements":[
	 {"type":"rich_text_section","elements":[{"type":"text","text":"hey "},{"type":"user","user_id":"U0"},{"type":"text","text":", "},
	  {"type":"channel","channel_id":"C2"},{"type":"text","text":" is "},{"type":"text","text":"done","style":{"bold":true}},
	  {"type":"text","text":" "},{"type":"emoji","name":"tada","unicode":"1f389"},{"type":"text","text":"\n"}]},
	 {"type":"rich_text_list","style":"bullet","indent":0,"elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"api ok"}]}]},
	 {"type":"rich_text_preformatted","elements":[{"type":"text","text":"make deploy"}]}]}]`),
}

// An app's message, like Terrafied's: most of Block Kit, and a fallback text.
var appMsg = slack.Message{
	TS: "1760000060.000200", BotID: "B1", Username: "Terrafied",
	Text: "FALLBACK Production Terraform Review Request",
	Blocks: jsontext.Value(`[
	 {"type":"header","text":{"type":"plain_text","text":":rotating_light: Production Terraform Review Request","emoji":true}},
	 {"type":"section","text":{"type":"mrkdwn","text":"*langwatch/langwatch-saas* · <https://github.com/x/y/commit/9b57ad7|9b57ad7> on main"},
	  "accessory":{"type":"button","text":{"type":"plain_text","text":"Inspect"},"action_id":"inspect","value":"1"}},
	 {"type":"section","fields":[{"type":"mrkdwn","text":"*Location*\nPalermo, Italy"},{"type":"mrkdwn","text":"*Device*\nSafari, macOS"}]},
	 {"type":"context","elements":[{"type":"image","image_url":"https://x/y.png","alt_text":"rogerio"},{"type":"mrkdwn","text":"rogeriochaves · Run #3531"},{"type":"plain_text","text":"Plan: 1 to add"}]},
	 {"type":"divider"},
	 {"type":"image","image_url":"https://x/plan.png","alt_text":"plan graph","image_width":1200,"image_height":800},
	 {"type":"actions","elements":[
	  {"type":"button","style":"primary","text":{"type":"plain_text","text":"Review Terraform Plan"},"action_id":"review"},
	  {"type":"button","text":{"type":"plain_text","text":"View on GitHub"},"url":"https://github.com/x/y/actions/runs/1"},
	  {"type":"static_select","placeholder":{"type":"plain_text","text":"Pick one"},"action_id":"pick"}]},
	 {"type":"input","label":{"type":"plain_text","text":"Why"},"element":{"type":"plain_text_input"}}]`),
}

// A Grafana alert, as legacy attachments, then two files.
var alertMsg = slack.Message{
	TS: "1760000120.000300", BotID: "B2", Username: "Grafana Alerts",
	Attachments: jsontext.Value(`[{"id":1,"color":"#E01E5A","fallback":"[FIRING:1] Fatal Errors","pretext":"heads up",
	 "author_name":"Grafana","title":"[FIRING:1] Fatal Errors","title_link":"https://grafana.example.com/alerting/1",
	 "text":"*What happened*: 37 groups are blocked","fields":[{"title":"alert","value":"Fatal Errors","short":true},{"title":"status","value":"firing","short":true}],
	 "footer":"Grafana v12.4.3","ts":1760000100},
	 {"color":"good","blocks":[{"type":"section","text":{"type":"mrkdwn","text":"recovered"}}]}]`),
	Files: jsontext.Value(`[{"id":"F1","name":"screenshot.png","title":"screenshot.png","mimetype":"image/png","filetype":"png","pretty_type":"PNG","size":1258291,"mode":"hosted","permalink":"https://x.slack.com/files/U1/F1/screenshot.png"},
	 {"id":"F2","mode":"tombstone"}]`),
}

func draw(t *testing.T, msg slack.Message, w int) string {
	t.Helper()
	m := fixture(t)
	var out []string
	m.st.Read(func(v store.View) {
		rows := renderMessage(&m.pal, v, &msg, w, true, time.Unix(1760000200, 0))
		for i, r := range rows {
			if r.Width() > w {
				t.Errorf("row %d is %d wide, over %d", i, r.Width(), w)
			}
		}
		out = plainFrame(rows)
	})
	text := strings.Join(out, "\n")
	if testing.Verbose() {
		t.Log("\n" + text)
	}
	return text
}

func wants(t *testing.T, text string, want ...string) {
	t.Helper()
	for _, s := range want {
		if !strings.Contains(text, s) {
			t.Errorf("lacks %q", s)
		}
	}
}

func TestRichTextBlocks(t *testing.T) {
	text := draw(t, humanMsg, 80)
	wants(t, text, "hey @alex, #alerts is done 🎉", "• api ok", "make deploy")
	if strings.Contains(text, ":tada:") || strings.Count(text, "make deploy") != 1 {
		t.Error("the text and its blocks both showed")
	}
}

func TestBlocks(t *testing.T) {
	text := draw(t, appMsg, 80)
	wants(t, text,
		"🚨 Production Terraform Review Request",
		"langwatch/langwatch-saas · 9b57ad7 on main",
		" Inspect ",
		"Location", "Palermo, Italy",
		"rogeriochaves · Run #3531  Plan: 1 to add",
		"────",
		"▣ plan graph 1200×800",
		" Review Terraform Plan    View on GitHub ↗    Pick one ▾ ",
		"unsupported block (input)",
	)
	if strings.Contains(text, "FALLBACK") {
		t.Error("the fallback text showed beside the blocks")
	}
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, "Location") && !strings.Contains(l, "Device") {
			t.Errorf("fields should sit side by side at 80: %q", l)
		}
	}
	narrow := draw(t, appMsg, 30)
	for _, l := range strings.Split(narrow, "\n") {
		if strings.Contains(l, "Location") && strings.Contains(l, "Device") {
			t.Errorf("fields should stack at 30: %q", l)
		}
	}
}

func TestAttachmentsAndFiles(t *testing.T) {
	text := draw(t, alertMsg, 80)
	wants(t, text,
		"heads up",
		"▌ Grafana",
		"▌ [FIRING:1] Fatal Errors",
		"▌ What happened: 37 groups are blocked",
		"▌ alert", "status",
		"▌ Grafana v12.4.3 · ",
		"▌ recovered",
		"▣ screenshot.png · PNG · 1.2 MB",
		"▤ this file was deleted",
	)
	for i, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) == "" {
			t.Errorf("row %d is blank, where an empty text or pretext was", i)
		}
	}
}

// Blocks it can't draw at all leave the text to say it.
func TestBlocksFallBack(t *testing.T) {
	msg := slack.Message{TS: "1760000000.000100", User: "U1", Text: "fill this in",
		Blocks: jsontext.Value(`[{"type":"input","element":{"type":"plain_text_input"}}]`)}
	text := draw(t, msg, 80)
	wants(t, text, "fill this in")
	if strings.Contains(text, "unsupported") {
		t.Error("showed the unsupported block over the text")
	}
}

func BenchmarkBlocks(b *testing.B) {
	m := fixture(b)
	m.st.Read(func(v store.View) {
		for b.Loop() {
			renderMessage(&m.pal, v, &appMsg, 80, true, time.Unix(1760000200, 0))
		}
	})
}
