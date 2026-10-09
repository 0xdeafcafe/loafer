package ui

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
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
	chin := canvas.T("  ", canvas.Style{}.Bg(tint(m.User+m.BotID+m.Username))) // the initials' second row
	if header {
		av := canvas.Style{}.Bg(tint(m.User + m.BotID + m.Username)).Fg(rgb(240, 236, 228)).With(canvas.Bold)
		f, pic := face(v, m)
		if !pic {
			f = canvas.T(initials(name), av)
		} else {
			chin = canvas.T("  ", ink.Text)
		}
		head := canvas.Row{f, canvas.T(" ", ink.Text), canvas.T(name, ink.Bright.With(canvas.Bold))}
		if app {
			head = append(head, canvas.T("  ", ink.Text), canvas.T("app", ink.Dim))
		}
		head = append(head, canvas.T("  "+clock(tsTime(m.TS), now), ink.Dim))
		rows = append(rows, head)
	}

	body := bodyRows(p, v, m, bodyW)
	if m.Edited != nil && len(body) > 0 {
		last := len(body) - 1
		body[last] = append(body[last], canvas.T(" (edited)", ink.Dim))
	}
	body = append(body, attachmentRows(p, v, m.Attachments, bodyW, now)...)
	body = append(body, fileRows(p, m.Files, bodyW)...)
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
			rows = append(rows, append(canvas.Row{chin, canvas.T(" ", ink.Text)}, r...))
			continue
		}
		rows = append(rows, append(canvas.Row{lead}, r...))
	}
	if header && len(body) == 0 {
		rows = append(rows, canvas.Row{chin})
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

func reactionRow(p *Palette, v store.View, rs []slack.Reaction) canvas.Row {
	var r canvas.Row
	for i, x := range rs {
		if i > 0 {
			r = append(r, canvas.T(" ", p.Main.Text))
		}
		e, std := emojiText(x.Name)
		chip, count := p.Chip, p.Chip.Fg(p.Main.Dim.FG)
		if slices.Contains(x.Users, v.Self()) {
			chip, count = p.Ask, p.Ask.Fg(p.Orange.FG).With(canvas.Bold) // yours stand out
		}
		glyph := chip
		if !std {
			glyph = chip.Fg(p.Main.Dim.FG)
		}
		r = append(r, canvas.T(" "+e+" ", glyph), canvas.T(strconv.Itoa(x.Count)+" ", count))
	}
	return r
}
