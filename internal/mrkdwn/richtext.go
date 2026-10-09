package mrkdwn

import (
	"cmp"
	"encoding/json/jsontext"
	"strconv"
	"strings"
	"time"

	"github.com/0xdeafcafe/photon/jsonx"
)

// Slack's rich_text blocks carry what mrkdwn does, already parsed: what
// the composer sent, so most people's messages come with one. They're read
// into the same lines, so they draw as text does.

// rtBlock is a rich_text block's part: a section, list, preformatted or
// quote. A list's elements are sections; the rest hold inline elements.
type rtBlock struct {
	Type     string         `json:"type"`
	Elements jsontext.Value `json:"elements"`
	Style    string         `json:"style"` // a list's "bullet" or "ordered"
	Indent   int            `json:"indent"`
	Offset   int            `json:"offset"`
}

type rtElem struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	URL         string `json:"url"`
	UserID      string `json:"user_id"`
	ChannelID   string `json:"channel_id"`
	UsergroupID string `json:"usergroup_id"`
	Range       string `json:"range"`   // a broadcast's here, channel or everyone
	Name        string `json:"name"`    // an emoji's
	Unicode     string `json:"unicode"` // an emoji's code points, "1f468-200d-1f4bb"
	Value       string `json:"value"`   // a colour's
	Fallback    string `json:"fallback"`
	Timestamp   int64  `json:"timestamp"` // a date's
	Style       struct {
		Bold, Italic, Strike, Code bool
	} `json:"style"`
}

// RichText reads a rich_text block's elements. ok is false when any of it
// isn't understood, so the caller can fall back to the message's text.
func RichText(raw jsontext.Value) (out []Line, ok bool) {
	var bs []rtBlock
	if jsonx.Unmarshal(raw, &bs) != nil {
		return nil, false
	}
	for _, b := range bs {
		switch b.Type {
		case "rich_text_section", "rich_text_quote":
			ls, ok := inlineLines(b.Elements)
			if !ok {
				return nil, false
			}
			for i := range ls {
				ls[i].Quote = b.Type == "rich_text_quote"
			}
			out = append(out, ls...)
		case "rich_text_preformatted":
			var es []rtElem
			if jsonx.Unmarshal(b.Elements, &es) != nil {
				return nil, false
			}
			var src strings.Builder
			for _, e := range es {
				src.WriteString(cmp.Or(e.Text, e.URL))
			}
			for i, l := range strings.Split(strings.TrimSuffix(src.String(), "\n"), "\n") {
				out = append(out, Line{Spans: []Span{{Kind: Code, Text: l}}, Pre: true, Start: i == 0})
			}
		case "rich_text_list":
			var items []rtBlock
			if jsonx.Unmarshal(b.Elements, &items) != nil {
				return nil, false
			}
			for i, it := range items {
				ls, ok := inlineLines(it.Elements)
				if !ok {
					return nil, false
				}
				if len(ls) == 0 {
					ls = []Line{{}}
				}
				ls[0].Bullet, ls[0].Depth = "•", b.Indent
				if b.Style == "ordered" {
					ls[0].Bullet = strconv.Itoa(b.Offset+i+1) + "."
				}
				out = append(out, ls...)
			}
		default:
			return nil, false
		}
	}
	return out, true
}

// inlineLines reads a section's elements, a new line at each "\n". A
// section's last newline only ends it, as Slack draws it.
func inlineLines(raw jsontext.Value) ([]Line, bool) {
	var es []rtElem
	if jsonx.Unmarshal(raw, &es) != nil {
		return nil, false
	}
	lines := []Line{{}}
	add := func(s Span) {
		l := &lines[len(lines)-1]
		l.Spans = append(l.Spans, s)
	}
	for _, e := range es {
		var mark Mark
		if e.Style.Bold {
			mark |= Bold
		}
		if e.Style.Italic {
			mark |= Italic
		}
		if e.Style.Strike {
			mark |= Strike
		}
		switch e.Type {
		case "text":
			kind := Text
			if e.Style.Code {
				kind = Code
			}
			for i, part := range strings.Split(e.Text, "\n") {
				if i > 0 {
					lines = append(lines, Line{})
				}
				if part != "" {
					add(Span{Kind: kind, Mark: mark, Text: part})
				}
			}
		case "link":
			add(Span{Kind: Link, Mark: mark, Text: cmp.Or(e.Text, e.URL), Target: e.URL})
		case "user":
			add(Span{Kind: User, Mark: mark, Target: e.UserID})
		case "channel":
			add(Span{Kind: Channel, Mark: mark, Target: e.ChannelID})
		case "usergroup":
			// The id stands in for the handle; the UI swaps in the store's (spanText).
			add(Span{Kind: Group, Mark: mark, Target: e.UsergroupID, Text: "@" + e.UsergroupID})
		case "broadcast":
			add(Span{Kind: Special, Mark: mark, Target: e.Range, Text: "@" + e.Range})
		case "emoji":
			if u := codePoints(e.Unicode); u != "" {
				add(Span{Kind: Text, Mark: mark, Text: u})
			} else {
				add(Span{Kind: Emoji, Mark: mark, Text: e.Name})
			}
		case "date":
			t := e.Fallback
			if t == "" {
				t = strings.ToLower(time.Unix(e.Timestamp, 0).Format("2 Jan 2006 15:04"))
			}
			add(Span{Kind: Text, Mark: mark, Text: t})
		case "color":
			add(Span{Kind: Text, Mark: mark, Text: e.Value})
		default:
			if e.Text == "" {
				return nil, false
			}
			add(Span{Kind: Text, Mark: mark, Text: e.Text})
		}
	}
	if n := len(lines); n > 1 && len(lines[n-1].Spans) == 0 {
		lines = lines[:n-1]
	}
	return lines, true
}

// codePoints is an emoji's "1f44d-1f3fd" as the characters, or "" if it
// isn't one.
func codePoints(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for h := range strings.SplitSeq(s, "-") {
		n, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return ""
		}
		b.WriteRune(rune(n))
	}
	return b.String()
}

// Plain reads Slack's plain_text: no markup but :emoji:, line by line.
func Plain(text string) []Line {
	var out []Line
	for l := range strings.SplitSeq(text, "\n") {
		var spans []Span
		from := 0 // where the text not yet in a span starts
		for i := 0; i < len(l); i++ {
			if l[i] != ':' {
				continue
			}
			end := strings.IndexByte(l[i+1:], ':')
			if end <= 0 || !emojiName(l[i+1:i+1+end]) {
				continue
			}
			if from < i {
				spans = append(spans, Span{Text: l[from:i]})
			}
			spans = append(spans, Span{Kind: Emoji, Text: l[i+1 : i+1+end]})
			i += end + 1
			from = i + 1
		}
		if from < len(l) {
			spans = append(spans, Span{Text: l[from:]})
		}
		out = append(out, Line{Spans: spans})
	}
	return out
}
