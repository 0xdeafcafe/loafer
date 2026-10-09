package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/canvas"
)

// Attaching (docs/ui.md, Files). A box's files are kept apart from its
// text, by the box's key (the conversation's id, or id/thread's, as drafts
// are), so they come and go with whichever box is swapped in without
// swap knowing. ctrl+o opens a prompt for a path; a path pasted into an
// empty box (a drag and drop, to a terminal) offers to attach instead.
// Sending is in upload.go.

// attached is a file waiting to be sent.
type attached struct {
	path, name string
	size       int64
}

type attachState struct {
	files map[string][]attached // by box
	ask   asker
	offer *offer
	up    *upload // the one going, if any
}

// asker is the path prompt.
type asker struct {
	on    bool
	box   string
	query []rune
	items []string // what completes the query, as paths
	at    int
}

// offer is a pasted path that turned out to be a file, waiting on a yes.
type offer struct {
	box   string
	files []attached
	text  string // what was pasted, if it's declined
}

type (
	fileMsg struct { // files looked at on disk
		box   string
		files []attached
		text  string // what was pasted, if it was
		paste bool
		err   error
	}
	listMsg struct { // what completes q
		q     string
		items []string
	}
)

var errNotFile = errors.New("not a file")

// boxKey is the box in front: the conversation's, or the thread's while
// it's swapped in.
func (m *Model) boxKey() string {
	if m.th.in {
		return m.th.conv + "/" + m.th.ts
	}
	return m.open
}

func (m *Model) addFiles(box string, files ...attached) {
	if m.att.files == nil {
		m.att.files = map[string][]attached{}
	}
	for _, f := range files {
		if !slices.ContainsFunc(m.att.files[box], func(x attached) bool { return x.path == f.path }) {
			m.att.files[box] = append(m.att.files[box], f)
		}
	}
}

// dropFile takes the box's last file off, if it has one.
func (m *Model) dropFile() bool {
	key := m.boxKey()
	files := m.att.files[key]
	if len(files) == 0 {
		return false
	}
	if m.att.files[key] = files[:len(files)-1]; len(files) == 1 {
		delete(m.att.files, key)
	}
	return true
}

// askFile opens the path prompt for the box in front.
func (m *Model) askFile() tea.Cmd {
	if m.editing != "" {
		return m.say("can't attach to an edit", true)
	}
	m.att.ask = asker{on: true, box: m.boxKey(), query: []rune("~/")}
	return listCmd("~/")
}

// statCmd looks at paths on disk, off the ui goroutine. pasted is what was
// pasted, when it was.
func statCmd(box string, paths []string, pasted string) tea.Cmd {
	return func() tea.Msg {
		files, err := statFiles(paths)
		return fileMsg{box, files, pasted, pasted != "", err}
	}
}

// statFiles says whether every path is a file Slack would take.
func statFiles(paths []string) ([]attached, error) {
	var out []attached
	for _, p := range paths {
		p, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		fi, err := os.Stat(p)
		name := filepath.Base(p)
		switch {
		case err != nil:
			return nil, err
		case !fi.Mode().IsRegular():
			return nil, fmt.Errorf("%s is not a file: %w", name, errNotFile)
		case fi.Size() == 0:
			return nil, fmt.Errorf("%s is empty", name)
		case fi.Size() > slack.MaxUpload:
			return nil, fmt.Errorf("%s is %s, and slack takes %s at most", name, size(fi.Size()), size(slack.MaxUpload))
		}
		out = append(out, attached{p, name, fi.Size()})
	}
	return out, nil
}

// listCmd finds what completes the typed path q.
func listCmd(q string) tea.Cmd { return func() tea.Msg { return listMsg{q, complete(q)} } }

// complete lists what q could be the start of: the entries of its folder
// that begin with its last part (folders end in /; dotfiles only if asked
// for), as q spells the folder.
func complete(q string) []string {
	dir, base := q[:strings.LastIndex(q, "/")+1], q[strings.LastIndex(q, "/")+1:]
	real := local(dir)
	if real == "" {
		real = "."
	}
	ents, err := os.ReadDir(real)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if !strings.HasPrefix(strings.ToLower(n), strings.ToLower(base)) || (n[0] == '.' && base == "") {
			continue
		}
		if fi, err := os.Stat(filepath.Join(real, n)); err == nil && fi.IsDir() { // symlinks to folders too
			n += "/"
		}
		out = append(out, dir+n)
		if len(out) == 100 {
			break
		}
	}
	return out
}

// local is a typed or pasted path as the disk has it: ~ is home, and
// file:// is what a drag from some apps gives.
func local(p string) string {
	if rest, ok := strings.CutPrefix(p, "file://"); ok {
		if u, err := url.PathUnescape(rest); err == nil {
			return u
		}
		return rest
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}

// pathsIn reads what a terminal pastes for a drop: one or more paths,
// quoted or with their spaces escaped, split by spaces or lines. Anything
// that doesn't start every word like a path isn't one.
func pathsIn(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 4096 {
		return nil
	}
	var out []string
	var cur strings.Builder
	var quote rune
	has := false
	flush := func() {
		if has {
			out = append(out, cur.String())
		}
		cur.Reset()
		has = false
	}
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		switch r := rs[i]; {
		case r == '\\' && quote != '\'' && i+1 < len(rs):
			i++
			cur.WriteRune(rs[i])
			has = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, has = r, true
		case unicode.IsSpace(r):
			flush()
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	flush()
	for i, p := range out {
		if !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, "file://") {
			return nil
		}
		out[i] = local(p)
	}
	return out
}

// pastedFiles takes a paste that may be files: into the prompt when it's
// open, else into an empty box, where it's looked at first. Anything else
// is text, and goes in as it always did.
func (m *Model) pastedFiles(content string) (tea.Cmd, bool) {
	paths := pathsIn(content)
	if a := &m.att.ask; a.on {
		q := strings.TrimSpace(content)
		if len(paths) == 1 {
			q = paths[0]
		}
		a.query, a.at = append(a.query, []rune(q)...), 0
		return listCmd(string(a.query)), true
	}
	if len(paths) == 0 || (m.focus != onCompose && m.focus != onReply) {
		return nil, false
	}
	var box string
	empty := false
	look := func() { box, empty = m.boxKey(), len(m.input) == 0 }
	if m.focus == onReply {
		m.inThread(look)
	} else {
		look()
	}
	if !empty {
		return nil, false
	}
	return statCmd(box, paths, content), true
}

// typeInto puts s in the box in front, as typed.
func (m *Model) typeInto(s string) {
	switch m.focus {
	case onCompose:
		m.insert(s)
		m.refreshPop()
	case onReply:
		m.inThread(func() { m.insert(s); m.refreshPop() })
	}
}

// attachKey takes the keys while the prompt is open, and the answer to an
// offer. ok says it was taken.
func (m *Model) attachKey(k tea.KeyPressMsg, s string) (cmd tea.Cmd, ok bool) {
	if o := m.att.offer; o != nil {
		m.att.offer = nil
		if s == "enter" {
			m.addFiles(o.box, o.files...)
			return nil, true
		}
		m.typeInto(o.text) // a no: it was only text
		return nil, s == "esc"
	}
	a := &m.att.ask
	if !a.on || s == "ctrl+c" || s == "f12" {
		return nil, false
	}
	switch s {
	case "esc":
		a.on = false
	case "enter":
		a.on = false
		return statCmd(a.box, []string{local(string(a.query))}, ""), true
	case "tab":
		if a.at < len(a.items) {
			a.query = []rune(a.items[a.at])
			a.at = 0
			return listCmd(string(a.query)), true
		}
	case "up", "ctrl+p":
		if n := len(a.items); n > 0 {
			a.at = (a.at + n - 1) % n
		}
	case "down":
		if n := len(a.items); n > 0 {
			a.at = (a.at + 1) % n
		}
	case "backspace":
		if len(a.query) > 0 {
			a.query, a.at = a.query[:len(a.query)-1], 0
			return listCmd(string(a.query)), true
		}
	default:
		if k.Text != "" {
			a.query, a.at = append(a.query, []rune(k.Text)...), 0
			return listCmd(string(a.query)), true
		}
	}
	return nil, true
}

// attachUpdate takes what the commands of this file, upload.go and
// download.go come back with.
func (m *Model) attachUpdate(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case listMsg:
		if a := &m.att.ask; a.on && msg.q == string(a.query) { // else it's of an older keystroke
			a.items, a.at = msg.items, 0
		}
	case fileMsg:
		switch {
		case msg.err == nil && msg.paste:
			m.att.offer = &offer{msg.box, msg.files, msg.text}
		case msg.err == nil:
			m.addFiles(msg.box, msg.files...)
		case msg.paste && (errors.Is(msg.err, fs.ErrNotExist) || errors.Is(msg.err, errNotFile)):
			m.typeInto(msg.text) // it only looked like a path
		default:
			return m.say("✗ "+msg.err.Error(), true)
		}
	case upMsg:
		return m.uploading(msg)
	case savedMsg:
		return m.saved(msg)
	}
	return nil
}

// --- drawing ---

// attachNote is the box's top label when it has something to say about
// files: an offer waiting for its answer.
func (m *Model) attachNote() (left, right string) {
	if o := m.att.offer; o != nil && o.box == m.boxKey() {
		names := make([]string, len(o.files))
		for i, f := range o.files {
			names[i] = f.name
		}
		return "attach " + strings.Join(names, ", ") + "?", "enter yes · any other key no"
	}
	return "", ""
}

// attachBox is the rows in the box above its text: a chip a file, then
// how an upload from this box is getting on. They're whole box rows.
func (m *Model) attachBox(edge, field canvas.Style, inner int) []canvas.Row {
	key := m.boxKey()
	var body []canvas.Row
	if files := m.att.files[key]; len(files) > 0 {
		var chips canvas.Row
		for _, f := range files {
			if len(chips) > 0 {
				chips = append(chips, canvas.T(" ", field))
			}
			chips = append(chips, canvas.T(" ▤ "+f.name+" · "+size(f.size)+" ", m.pal.Chip))
		}
		body = canvas.Wrap(chips, inner)
		body = body[:min(len(body), 3)] // shortcut: three rows of chips, then they're not shown, until they're sent
	}
	if u := m.att.up; u != nil && u.key == key {
		body = append(body, u.row(m, field))
	}
	out := make([]canvas.Row, len(body))
	for i, l := range body {
		row := canvas.Row{canvas.T("│", edge), canvas.T(" ", field), canvas.T("  ", field)}
		out[i] = append(append(row, canvas.Fit(l, inner, field)...), canvas.T(" ", field), canvas.T("│", edge))
	}
	return out
}

// overlayAttach draws the path prompt over the frame, which goes faint
// behind it, as overlayReact does.
func (m *Model) overlayAttach(frame []canvas.Row) []canvas.Row {
	ink, a := m.pal.Main, &m.att.ask
	bw := min(m.w-4, 64)
	if bw < 30 {
		return frame
	}
	inner := bw - 4
	top := min(m.h/8, 4)
	listH := max(1, min(len(a.items), m.h*2/3-5, popCap))
	edge, fill := m.pal.Orange, ink.Text.Bg(ink.Sel.BG)

	line := func(r canvas.Row) canvas.Row {
		return append(append(canvas.Row{canvas.T("│ ", edge.Bg(fill.BG))}, canvas.Fit(r, inner, fill)...), canvas.T(" │", edge.Bg(fill.BG)))
	}
	edgeRow := func(l, label, r string) canvas.Row {
		row := canvas.Row{canvas.T(l+"─ ", edge), canvas.T(label, ink.Sub.With(canvas.Bold))}
		return append(row, canvas.T(" "+strings.Repeat("─", max(0, bw-row.Width()-2))+r, edge))
	}
	box := []canvas.Row{edgeRow("╭", "▤ Attach a file", "╮")}
	box = append(box, line(canvas.Row{canvas.T("❯ ", fill.Fg(m.pal.Orange.FG).With(canvas.Bold)), canvas.T(string(a.query), fill.Fg(ink.Bright.FG)), canvas.T("▏", fill.Fg(m.pal.Orange.FG))}))
	box = append(box, line(canvas.Row{canvas.T(strings.Repeat("─", inner), fill.Fg(ink.Faint.FG))}))
	if len(a.items) == 0 {
		box = append(box, line(canvas.Row{canvas.T("  nothing starts like that", fill.Fg(ink.Dim.FG))}))
	}
	for i := max(0, a.at-listH+1); i < min(len(a.items), max(0, a.at-listH+1)+listH); i++ {
		base, mark := fill, canvas.T("  ", fill)
		if i == a.at {
			base = fill.Bg(ink.Hover.BG)
			mark = canvas.T("▍ ", base.Fg(m.pal.Orange.FG))
		}
		box = append(box, line(canvas.Row{mark, canvas.T(a.items[i], base.Fg(ink.Text.FG))}))
	}
	box = append(box, edgeRow("╰", "↑↓ choose · tab complete · enter attach · esc close", "╯"))

	out := make([]canvas.Row, len(frame))
	for i, r := range frame {
		out[i] = faint(r, ink.Faint.FG)
	}
	left := (m.w - bw) / 2
	for i, r := range box {
		if top+i < len(out)-1 {
			out[top+i] = canvas.Splice(out[top+i], left, r)
		}
	}
	return out
}
