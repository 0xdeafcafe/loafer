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
//	████ Name  app  21:49
//	████ body, wrapped at the pane less the gutter
//	     ▌ attachment, behind a bar in its colour
//	     👍 1  ☺+          reactions
//	     ↩ 4 replies · last 16:47
//
// The avatar is avatarW×avatarH cells, a picture or initials on a tint
// alike, beside the name and the first line; a follow-up from the same
// author within five minutes has no header and no avatar.

const (
	avatarW, avatarH = 4, 2 // about square: a cell is twice as tall as wide
	gutter           = avatarW + 1
)

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
	var av [avatarH]canvas.Row
	if header {
		av = avatar(p, v, m, name)
		head := append(av[0], canvas.T(" ", ink.Text), canvas.T(name, ink.Bright.With(canvas.Bold)))
		if g := statusGlyph(v, m.User); g != "" {
			head = append(head, canvas.T(" "+g, ink.Text))
		}
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
	if len(m.PinnedTo) > 0 {
		body = append(body, canvas.Row{canvas.T("⚑ pinned", ink.Dim)})
	}
	if len(m.Reactions) > 0 {
		body = append(body, reactionRows(p, v, m.Reactions, bodyW)...)
	}
	if m.ReplyCount > 0 {
		t := strconv.Itoa(m.ReplyCount) + " replies"
		if m.ReplyCount == 1 {
			t = "1 reply"
		}
		if m.LatestReply != "" {
			t += " · last " + clock(tsTime(m.LatestReply), now)
		}
		body = append(body, append(canvas.Row{canvas.T("↩ "+t, p.Blue)}, repliers(p, v, m.ReplyUsers)...))
	}

	for i, r := range body {
		if header && i+1 < avatarH {
			// The avatar's lower rows sit beside the first lines.
			rows = append(rows, append(append(av[i+1], canvas.T(" ", ink.Text)), r...))
			continue
		}
		rows = append(rows, append(canvas.Row{pad}, r...))
	}
	for i := len(body) + 1; header && i < avatarH; i++ {
		rows = append(rows, av[i])
	}
	return rows
}

// avatar is m's avatar as avatarH rows each avatarW wide: its picture,
// centred, where it's landed; else its initials, bold on its tint, as
// Slack's letter avatars are.
func avatar(p *Palette, v store.View, m *slack.Message, name string) (rows [avatarH]canvas.Row) {
	ground := p.Main.Text
	if pic, ok := face(v, m); ok {
		l := (avatarW - pic.Cols) / 2
		for r := range rows {
			if r < pic.Rows {
				rows[r] = canvas.Row{canvas.T(strings.Repeat(" ", l), ground), picSeg(pic, r), canvas.T(strings.Repeat(" ", avatarW-l-pic.Cols), ground)}
			} else {
				rows[r] = canvas.Row{canvas.T(strings.Repeat(" ", avatarW), ground)}
			}
		}
		return rows
	}
	tile := canvas.Style{}.Bg(tint(m.User + m.BotID + m.Username)).Fg(rgb(240, 236, 228))
	rows[0] = canvas.Fit(canvas.Row{canvas.T(" "+initials(name), tile.With(canvas.Bold))}, avatarW, tile)
	for r := 1; r < avatarH; r++ {
		rows[r] = canvas.Row{canvas.T(strings.Repeat(" ", avatarW), tile)}
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

// reactionRows are a message.s reactions as Slack.s pills, the emoji and
// its count on a chip, yours warm with an orange count, wrapped a whole
// pill at a time to w.
func reactionRows(p *Palette, v store.View, rs []slack.Reaction, w int) []canvas.Row {
	var out []canvas.Row
	var r canvas.Row
	for _, x := range rs {
		e, std := emojiText(x.Name)
		chip, count := p.Chip, p.Chip.Fg(p.Main.Sub.FG)
		if slices.Contains(x.Users, v.Self()) {
			chip, count = p.Ask, p.Ask.Fg(p.Orange.FG).With(canvas.Bold) // yours stand out
		}
		glyph := canvas.Row{canvas.T(" "+e+" ", chip)}
		if !std {
			glyph = canvas.Row{canvas.T(" "+e+" ", chip.Fg(p.Main.Dim.FG))}
			if pic, ok := inlinePic(v.Emoji(x.Name)); ok {
				glyph = canvas.Row{canvas.T(" ", chip), pic, canvas.T(" ", chip)}
			}
		}
		pill := append(glyph, canvas.T(strconv.Itoa(x.Count)+" ", count))
		if len(r) > 0 && r.Width()+1+pill.Width() > w {
			out, r = append(out, r), nil
		}
		if len(r) > 0 {
			r = append(r, canvas.T(" ", p.Main.Text))
		}
		r = append(r, pill...)
	}
	return append(out, r)
}
