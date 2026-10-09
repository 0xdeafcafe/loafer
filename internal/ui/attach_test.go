package ui

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

var cCtrlO = tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}

// feed runs cmd as bubbletea would and hands its message to the model. One
// hop: what that asks for next is a flash's timer, which it would wait out.
func feed(m *Model, cmd tea.Cmd) {
	if cmd != nil {
		if msg := cmd(); msg != nil {
			m.Update(msg)
		}
	}
}

func frameText(m *Model) string { return strings.Join(plainFrame(m.render()), "\n") }

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPathsIn(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"/a/b.png", []string{"/a/b.png"}},
		{`/a/My\ File.png`, []string{"/a/My File.png"}},
		{`'/a/My File.png' "/b/c d.txt"`, []string{"/a/My File.png", "/b/c d.txt"}},
		{"/a/b.png\n/c/d.png\n", []string{"/a/b.png", "/c/d.png"}},
		{"file:///a/My%20File.png", []string{"/a/My File.png"}},
		{"hello /a/b.png", nil},
		{"just some words", nil},
		{"", nil},
		{"   ", nil},
	} {
		if got := pathsIn(c.in); !slices.Equal(got, c.want) {
			t.Errorf("%q: %q, want %q", c.in, got, c.want)
		}
	}
}

func TestComplete(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "notes.txt", "x")
	write(t, dir, "Notebook.md", "x")
	write(t, dir, ".secret", "x")
	if err := os.Mkdir(filepath.Join(dir, "photos"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)
	for q, want := range map[string][]string{
		"~/":    {"~/Notebook.md", "~/notes.txt", "~/photos/"}, // dotfiles only when asked for
		"~/no":  {"~/Notebook.md", "~/notes.txt"},              // any case
		"~/.":   {"~/.secret"},
		"~/zzz": nil,
	} {
		if got := complete(q); !slices.Equal(got, want) {
			t.Errorf("%q: %q, want %q", q, got, want)
		}
	}
}

func TestStatFiles(t *testing.T) {
	dir := t.TempDir()
	ok := write(t, dir, "a.txt", "abc")
	empty := write(t, dir, "empty", "")
	big := write(t, dir, "big", "")
	if err := os.Truncate(big, slack.MaxUpload+1); err != nil { // sparse: costs nothing
		t.Fatal(err)
	}
	if got, err := statFiles([]string{ok}); err != nil || len(got) != 1 || got[0].name != "a.txt" || got[0].size != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	for path, want := range map[string]string{big: "slack takes 1.0 GB at most", empty: "is empty", dir: "not a file"} {
		if _, err := statFiles([]string{path}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", filepath.Base(path), err, want)
		}
	}
}

// ctrl+o, a path, enter: a chip in the box; backspace at the start of an
// empty box takes it off again.
func TestAttachPrompt(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "report.pdf", "%PDF")
	t.Setenv("HOME", dir)
	m := composeFixture(t)

	_, cmd := m.Update(cCtrlO)
	if !m.att.ask.on {
		t.Fatal("ctrl+o opened nothing")
	}
	feed(m, cmd)
	if len(m.att.ask.items) != 1 || !strings.Contains(frameText(m), "Attach a file") || !strings.Contains(frameText(m), "~/report.pdf") {
		t.Fatalf("no completion of ~/:\n%s", frameText(m))
	}
	_, cmd = m.Update(tab) // tab completes
	feed(m, cmd)
	if string(m.att.ask.query) != "~/report.pdf" {
		t.Fatalf("query %q", string(m.att.ask.query))
	}
	_, cmd = m.Update(cEnter)
	feed(m, cmd)
	if m.att.ask.on || len(m.att.files[m.open]) != 1 || m.att.files[m.open][0].path != path {
		t.Fatalf("not attached: %+v", m.att)
	}
	if text := frameText(m); !strings.Contains(text, "▤ report.pdf · 4 B") {
		t.Fatalf("no chip:\n%s", text)
	}

	typeText(m, "x")
	press(m, cBackspace, cBackspace) // the letter, then the chip
	if len(m.att.files) != 0 || strings.Contains(frameText(m), "report.pdf") {
		t.Fatalf("chip stayed: %+v", m.att.files)
	}

	// A file that isn't there is said, not attached.
	press(m, cCtrlO)
	press(m, cBackspace, cBackspace)
	typeText(m, "/nope/nothing")
	_, cmd = m.Update(cEnter)
	feed(m, cmd)
	if len(m.att.files) != 0 || !m.flashErr || !strings.Contains(m.flash, "no such file") {
		t.Fatalf("flash %q", m.flash)
	}
}

// A path pasted into an empty box is offered; anything else is text.
func TestPasteOffer(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "my notes.txt", "hi")
	m := composeFixture(t)

	_, cmd := m.Update(tea.PasteMsg{Content: "'" + path + "'"})
	feed(m, cmd)
	if m.att.offer == nil || len(m.input) != 0 || !strings.Contains(frameText(m), "attach my notes.txt?") {
		t.Fatalf("no offer:\n%s", frameText(m))
	}
	press(m, cEnter)
	if m.att.offer != nil || len(m.att.files[m.open]) != 1 || len(m.input) != 0 {
		t.Fatalf("not taken: %+v input %q", m.att, string(m.input))
	}
	m.att.files = nil

	// Declined, it was only text after all, and the key goes on to do its job.
	_, cmd = m.Update(tea.PasteMsg{Content: path})
	feed(m, cmd)
	press(m, r('!'))
	if m.att.offer != nil || len(m.att.files) != 0 || string(m.input) != path+"!" {
		t.Fatalf("declined: %q", string(m.input))
	}

	// Not a file, or not an empty box: text.
	m.load(nil, nil)
	_, cmd = m.Update(tea.PasteMsg{Content: "/no/such/thing"})
	feed(m, cmd)
	if string(m.input) != "/no/such/thing" || m.att.offer != nil {
		t.Fatalf("a missing path: %q", string(m.input))
	}
	m.load([]rune("see "), nil)
	_, cmd = m.Update(tea.PasteMsg{Content: path})
	feed(m, cmd)
	if string(m.input) != "see "+path || m.att.offer != nil {
		t.Fatalf("pasted into text: %q", string(m.input))
	}
}

// Files belong to their box: the thread's aren't the conversation's.
func TestAttachBoxes(t *testing.T) {
	m := composeFixture(t)
	m.addFiles("C1", attached{path: "/a", name: "a", size: 1})
	m.addFiles("C1", attached{path: "/a", name: "a", size: 1}) // not twice
	m.addFiles("C1/1.0", attached{path: "/b", name: "b", size: 1})
	if len(m.att.files["C1"]) != 1 || m.boxKey() != "C1" {
		t.Fatalf("%+v %q", m.att.files, m.boxKey())
	}
	m.th = threadPane{conv: "C1", ts: "1.0", heights: map[string]int{}}
	m.inThread(func() {
		if m.boxKey() != "C1/1.0" || !m.dropFile() || m.dropFile() {
			t.Errorf("thread box %q", m.boxKey())
		}
	})
	if len(m.att.files["C1"]) != 1 || len(m.att.files["C1/1.0"]) != 0 {
		t.Fatalf("%+v", m.att.files)
	}
}

func TestPutBack(t *testing.T) {
	m := composeFixture(t)
	u := &upload{key: "C1", conv: "C1", files: []attached{{path: "/a", name: "a", size: 1}}, input: []rune("the notes")}
	m.putBack(u)
	if string(m.input) != "the notes" || len(m.att.files["C1"]) != 1 {
		t.Fatalf("%q %+v", string(m.input), m.att.files)
	}
	m.att.files = nil
	m.load([]rune("typed since"), nil)
	m.putBack(u)
	if string(m.input) != "typed since" { // what's been written since wins
		t.Fatalf("%q", string(m.input))
	}
	u.key, u.conv = "D1", "D1" // out of sight: a draft
	m.putBack(u)
	if string(m.drafts["D1"].text) != "the notes" || len(m.att.files["D1"]) != 1 {
		t.Fatalf("%+v", m.drafts)
	}
}

func TestDownloadable(t *testing.T) {
	raw := jsontext.Value(`[{"name":"a.png","url_private_download":"https://files.slack.com/a"},
		{"name":"b","url_private":"https://files.slack.com/b"},
		{"name":"gone","mode":"tombstone"},
		{"name":"drive","mode":"external"}]`)
	got := downloadable(raw)
	if len(got) != 2 || got[0].url() != "https://files.slack.com/a" || got[1].url() != "https://files.slack.com/b" {
		t.Fatalf("%+v", got)
	}
	if downloadable(nil) != nil || downloadable(jsontext.Value(`nonsense`)) != nil {
		t.Fatal("something from nothing")
	}
}

// A name that's taken gets (1), then (2), and a file can't climb out of
// the folder.
func TestCreate(t *testing.T) {
	dir := t.TempDir()
	var got []string
	for range 3 {
		f, err := create(dir, "report.final.pdf")
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		got = append(got, filepath.Base(f.Name()))
	}
	if want := []string{"report.final.pdf", "report.final (1).pdf", "report.final (2).pdf"}; !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}
	for in, want := range map[string]string{"../../etc/passwd": "passwd", "": "file", "..": "file", "a/b.txt": "b.txt"} {
		if got := fileName(dlFile{Name: in}); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
