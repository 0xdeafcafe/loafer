package ui

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/canvas"
)

// Sending files. One upload goes at a time, from a box that's been
// emptied: its text, kept here, is the comment, and goes back with its
// files if Slack says no. The upload reads from disk as it sends, and
// tells the model how far it has got through a channel that holds one
// report: a command waits on it and hands each to Update, which waits
// again, so there's no timer and a slow screen can't pile them up.

type upload struct {
	key, conv, ts string // the box, and where it goes: ts is the thread's parent
	also          bool
	files         []attached
	text          string
	input         []rune // the box as it was, for putting back
	ments         []mention
	sent, total   int64
	ch            chan upMsg
}

// upMsg is how an upload is getting on, or the end of it.
type upMsg struct {
	sent, total int64
	done        bool
	err         error
}

// tell is the most often progress is reported.
const tell = 100 * time.Millisecond

// sendFiles sends the box's files with its text, if it has files. ok says
// it was them that enter was for.
func (m *Model) sendFiles() (cmd tea.Cmd, ok bool) {
	key := m.boxKey()
	files := m.att.files[key]
	if len(files) == 0 || m.editing != "" || m.open == "" {
		return nil, false
	}
	if m.att.up != nil {
		return m.say("one upload at a time", true), true
	}
	u := &upload{key: key, conv: m.open, files: files, text: strings.TrimSpace(encode(m.input, m.ments)),
		input: m.input, ments: m.ments, ch: make(chan upMsg, 1)}
	if m.th.in {
		u.conv, u.ts, u.also = m.th.conv, m.th.ts, m.th.also
		m.th.also = false
	}
	for _, f := range files {
		u.total += f.size
	}
	delete(m.att.files, key)
	m.load(nil, nil)
	m.att.up = u
	return m.runUpload(u), true
}

// runUpload starts the upload and waits for its first word.
func (m *Model) runUpload(u *upload) tea.Cmd {
	to := slack.Share{Channel: u.conv, ThreadTS: u.ts, Text: u.text, Also: u.also}
	files := make([]slack.UploadFile, len(u.files))
	for i, f := range u.files {
		files[i] = slack.UploadFile{Path: f.path, Name: f.name, Size: f.size}
	}
	ctx, ch := m.ctx, u.ch
	return func() tea.Msg {
		go func() {
			var last time.Time
			err := upFiles(ctx, m.api, to, files, func(sent, total int64) {
				if now := time.Now(); now.Sub(last) >= tell {
					last = now
					select {
					case ch <- upMsg{sent: sent, total: total}:
					default: // the last is still unread; this one's stale already
					}
				}
			})
			select {
			case ch <- upMsg{done: true, err: err}:
			case <-ctx.Done():
			}
		}()
		return waitUp(ctx, ch)()
	}
}

// upFiles looks again at each file, which may have changed since it was
// attached, and sends them.
func upFiles(ctx context.Context, api *slack.Client, to slack.Share, files []slack.UploadFile, progress func(sent, total int64)) error {
	for i, f := range files {
		now, err := statFiles([]string{f.Path})
		if err != nil {
			return err
		}
		files[i].Size = now[0].size
	}
	return api.Upload(ctx, to, files, progress)
}

func waitUp(ctx context.Context, ch chan upMsg) tea.Cmd {
	return func() tea.Msg {
		select {
		case msg := <-ch:
			return msg
		case <-ctx.Done():
			return nil
		}
	}
}

// uploading takes a word from the upload: how far it has got, or that it's
// over. The message itself comes over the websocket like anyone's.
func (m *Model) uploading(msg upMsg) tea.Cmd {
	u := m.att.up
	if u == nil {
		return nil
	}
	if !msg.done {
		u.sent, u.total = msg.sent, msg.total
		return waitUp(m.ctx, u.ch)
	}
	m.att.up = nil
	if msg.err == nil {
		return m.markRead()
	}
	m.putBack(u)
	return m.say("✗ couldn't upload: "+msg.err.Error(), true)
}

// putBack returns a failed upload's files and words to their box, unless
// that has been written in since, and to its draft if it's out of sight.
func (m *Model) putBack(u *upload) {
	m.addFiles(u.key, u.files...)
	put := func() {
		if len(m.input) == 0 {
			m.load(u.input, u.ments)
		}
	}
	switch {
	case u.ts == "" && u.conv == m.open:
		put()
	case u.ts != "" && u.conv == m.th.conv && u.ts == m.th.ts:
		m.inThread(put)
	default:
		if _, had := m.drafts[u.key]; !had {
			m.drafts[u.key] = draft{u.input, u.ments}
		}
	}
}

// row is the upload's line in its box: what's going, a bar and a percent.
func (u *upload) row(m *Model, field canvas.Style) canvas.Row {
	ink := m.pal.Main
	what := u.files[0].name
	if len(u.files) > 1 {
		what += " and " + strconv.Itoa(len(u.files)-1) + " more"
	}
	pc := 0
	if u.total > 0 {
		pc = int(u.sent * 100 / u.total)
	}
	const bar = 16
	n := pc * bar / 100
	return canvas.Row{
		canvas.T("↑ ", field.Fg(m.pal.Orange.FG)), canvas.T(what+"  ", field.Fg(ink.Text.FG)),
		canvas.T(strings.Repeat("█", n), field.Fg(m.pal.Orange.FG)), canvas.T(strings.Repeat("░", bar-n), field.Fg(ink.Faint.FG)),
		canvas.T(" "+strconv.Itoa(pc)+"%", field.Fg(ink.Dim.FG)),
	}
}
