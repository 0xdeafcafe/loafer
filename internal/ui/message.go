package ui

import (
	"encoding/json/jsontext"
	"strconv"
	"strings"
	"time"

	"github.com/0xdeafcafe/loafer/internal/mrkdwn"
	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/jsonx"
	"github.com/0xdeafcafe/photon/theme"
)

// A message is drawn as (docs/ui.md, Main screen):
//
//	██ Name  app  21:49
//	██ body, wrapped at the pane less the gutter
//	   ▌ attachment, behind a bar in its colour
//	   👍 1  ☺+          reactions
//	   ↩ 4 replies · last 16:47
//
// The avatar is 2×2 cells; a follow-up from the same author within five
// minutes has no header and no avatar.

const gutter = 3 // avatar (2) and a space

// tsTime reads a Slack ts ("1696789123.000200").
func tsTime(ts string) time.Time {
	sec, frac, _ := strings.Cut(ts, ".")
	s, _ := strconv.ParseInt(sec, 10, 64)
	us, _ := strconv.ParseInt((frac + "000000")[:6], 10, 64)
	return time.Unix(s, us*1000)
}

// followsOn says whether m continues prev without a header of its own.
func followsOn(prev, m *slack.Message) bool {
	if prev == nil || prev.Subtype != "" || m.Subtype != "" {
		return false
	}
	who := func(x *slack.Message) string { return x.User + x.BotID + x.Username }
	return who(prev) == who(m) && tsTime(m.TS).Sub(tsTime(prev.TS)) < 5*time.Minute
}

// author is who a message is from, and whether it's an app.
func author(v store.View, m *slack.Message) (name string, app bool) {
	switch {
	case m.Username != "":
		return m.Username, true
	case m.BotProfile != nil && m.BotProfile.Name != "":
		return m.BotProfile.Name, true
	case m.User != "":
		p := v.Person(m.User)
		return p.Name, p.Bot
	}
	return "unknown", m.BotID != ""
}

// initials is the avatar's stand-in until images draw: up to two letters.
func initials(name string) string {
	f := strings.Fields(name)
	switch {
	case len(f) >= 2:
		return strings.ToUpper(string([]rune(f[0])[:1]) + string([]rune(f[1])[:1]))
	case len(f) == 1 && len([]rune(f[0])) >= 2:
		r := []rune(f[0])
		return strings.ToUpper(string(r[0])) + string(r[1])
	}
	return "··"
}

// tint is a stable avatar colour from an id.
func tint(id string) theme.RGB {
	var h uint32 = 2166136261
	for i := 0; i < len(id); i++ {
		h = (h ^ uint32(id[i])) * 16777619
	}
	shades := []theme.RGB{rgb(120, 78, 98), rgb(74, 104, 132), rgb(86, 120, 92), rgb(140, 104, 64), rgb(104, 88, 140), rgb(64, 118, 120)}
	return shades[h%uint32(len(shades))]
}

// renderMessage draws m at width w; header says whether it gets a
// header (it's not a follow-on).
func renderMessage(p *Palette, v store.View, m *slack.Message, w int, header bool, now time.Time) []canvas.Row {
	ink := p.Main
	var rows []canvas.Row
	bodyW := max(10, w-gutter)
	pad := canvas.T(strings.Repeat(" ", gutter), ink.Text)

	name, app := author(v, m)
	if header {
		av := canvas.Style{}.Bg(tint(m.User + m.BotID + m.Username)).Fg(rgb(240, 236, 228)).With(canvas.Bold)
		head := canvas.Row{canvas.T(initials(name), av), canvas.T(" ", ink.Text), canvas.T(name, ink.Bright.With(canvas.Bold))}
		if app {
			head = append(head, canvas.T("  ", ink.Text), canvas.T("app", ink.Dim))
		}
		head = append(head, canvas.T("  "+clock(tsTime(m.TS), now), ink.Dim))
		rows = append(rows, head)
	}

	body := textRows(p, v, mrkdwn.Parse(m.Text), bodyW)
	if m.Edited != nil && len(body) > 0 {
		last := len(body) - 1
		body[last] = append(body[last], canvas.T(" (edited)", ink.Dim))
	}
	body = append(body, attachmentRows(p, v, m.Attachments, bodyW)...)
	if len(m.Reactions) > 0 {
		body = append(body, reactionRow(p, v, m.Reactions))
	}
	if m.ReplyCount > 0 {
		t := strconv.Itoa(m.ReplyCount) + " replies"
		if m.ReplyCount == 1 {
			t = "1 reply"
		}
		if m.LatestReply != "" {
			t += " · last " + clock(tsTime(m.LatestReply), now)
		}
		body = append(body, canvas.Row{canvas.T("↩ "+t, p.Blue)})
	}

	for i, r := range body {
		lead := pad
		if header && i == 0 {
			// The avatar's second row sits beside the first line.
			av := canvas.Style{}.Bg(tint(m.User + m.BotID + m.Username))
			lead = canvas.T("  ", av)
			rows = append(rows, append(canvas.Row{lead, canvas.T(" ", ink.Text)}, r...))
			continue
		}
		rows = append(rows, append(canvas.Row{lead}, r...))
	}
	if header && len(body) == 0 {
		rows = append(rows, canvas.Row{canvas.T("  ", canvas.Style{}.Bg(tint(m.User+m.BotID+m.Username)))})
	}
	return rows
}

// clock is a message's time: the time today, else the day and time.
func clock(t, now time.Time) string {
	if y, d := t.YearDay(), now.YearDay(); t.Year() == now.Year() && y == d {
		return t.Format("15:04")
	}
	if now.Sub(t) < 6*24*time.Hour {
		return strings.ToLower(t.Format("Mon 15:04"))
	}
	return strings.ToLower(t.Format("2 Jan 15:04"))
}

// emoji is a shortcode as a character, when it's one of the common ones;
// else ":name:" and a zero style, so the caller dims it.
// ponytail: a short table, until the full Unicode shortcode list (and
// custom emoji as images) lands with the Block Kit step.
func emoji(name string) (string, canvas.Style) {
	if e, ok := emojiTable[name]; ok {
		return e, canvas.Style{}.With(canvas.Bold)
	}
	return ":" + name + ":", canvas.Style{}
}

var emojiTable = map[string]string{
	"+1": "👍", "thumbsup": "👍", "-1": "👎", "eyes": "👀", "tada": "🎉", "pray": "🙏", "fire": "🔥",
	"white_check_mark": "✅", "heavy_check_mark": "✔️", "x": "❌", "warning": "⚠️", "rotating_light": "🚨",
	"rocket": "🚀", "heart": "❤️", "joy": "😂", "smile": "😄", "slightly_smiling_face": "🙂",
	"stuck_out_tongue_winking_eye": "😜", "thinking_face": "🤔", "100": "💯", "wave": "👋", "ok_hand": "👌",
	"raised_hands": "🙌", "clap": "👏", "sweat_smile": "😅", "point_up": "☝️", "speech_balloon": "💬",
	"bangbang": "‼️", "sob": "😭", "zap": "⚡", "bug": "🐛", "memo": "📝", "link": "🔗", "lock": "🔒",
}

// attachment is a legacy attachment, as bots like Grafana still send.
type attachment struct {
	Color     string `json:"color"`
	Pretext   string `json:"pretext"`
	Title     string `json:"title"`
	TitleLink string `json:"title_link"`
	Text      string `json:"text"`
	Fallback  string `json:"fallback"`
	Footer    string `json:"footer"`
	Fields    []struct {
		Title string `json:"title"`
		Value string `json:"value"`
	} `json:"fields"`
}

func attachmentRows(p *Palette, v store.View, raw jsontext.Value, w int) []canvas.Row {
	if len(raw) == 0 {
		return nil
	}
	var as []attachment
	if jsonx.Unmarshal(raw, &as) != nil {
		return nil
	}
	var out []canvas.Row
	for _, a := range as {
		bar := canvas.T("▌ ", p.Main.Faint)
		if c, ok := hexRGB(a.Color); ok {
			bar = canvas.T("▌ ", p.Main.Text.Fg(c))
		}
		var inner []canvas.Row
		add := func(text string, st *canvas.Style) {
			for _, l := range mrkdwn.Parse(text) {
				rows := lineRows(p, v, l, w-2)
				if st != nil {
					for i := range rows {
						rows[i] = canvas.Restyle(rows[i], *st)
					}
				}
				inner = append(inner, rows...)
			}
		}
		if a.Pretext != "" {
			add(a.Pretext, nil)
		}
		if a.Title != "" {
			st := p.Blue.With(canvas.Bold)
			st.Link = a.TitleLink
			add(a.Title, &st)
		}
		if a.Text != "" {
			add(a.Text, nil)
		} else if a.Title == "" && a.Fallback != "" {
			add(a.Fallback, nil)
		}
		for _, f := range a.Fields {
			bold := p.Main.Text.With(canvas.Bold)
			add(f.Title, &bold)
			add(f.Value, nil)
		}
		if a.Footer != "" {
			add(a.Footer, &p.Main.Dim)
		}
		for _, r := range inner {
			out = append(out, append(canvas.Row{bar}, r...))
		}
	}
	return out
}

// hexRGB reads Slack's attachment colour ("#36a64f", "36a64f", or a name).
func hexRGB(s string) (theme.RGB, bool) {
	switch s {
	case "good":
		return rgb(46, 182, 125), true
	case "warning":
		return rgb(236, 178, 46), true
	case "danger":
		return rgb(224, 30, 90), true
	}
	s = strings.TrimPrefix(s, "#")
	n, err := strconv.ParseUint(s, 16, 32)
	if len(s) != 6 || err != nil {
		return theme.RGB{}, false
	}
	return rgb(uint8(n>>16), uint8(n>>8), uint8(n)), true
}

func reactionRow(p *Palette, v store.View, rs []slack.Reaction) canvas.Row {
	var r canvas.Row
	for i, x := range rs {
		if i > 0 {
			r = append(r, canvas.T(" ", p.Main.Text))
		}
		e, _ := emoji(x.Name)
		mine := false
		for _, u := range x.Users {
			mine = mine || u == v.Self()
		}
		count := p.Chip.Fg(p.Main.Dim.FG)
		if mine {
			count = p.Chip.Fg(p.Orange.FG).With(canvas.Bold)
		}
		r = append(r, canvas.T(" "+e+" ", p.Chip), canvas.T(strconv.Itoa(x.Count)+" ", count))
	}
	return r
}
