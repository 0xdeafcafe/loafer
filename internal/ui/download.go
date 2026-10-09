package ui

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/photon/jsonx"
)

// Files out: s saves the selected message's files to ~/Downloads, O saves
// them and opens them with the system's own `open`.

// dlFile is the part of a file a download needs.
type dlFile struct {
	Name    string `json:"name"`
	Mode    string `json:"mode"`
	Private string `json:"url_private"`
	Direct  string `json:"url_private_download"`
}

// savedMsg is a save's end: the paths written, up to any failure.
type savedMsg struct {
	paths []string
	open  bool
	err   error
}

// downloadable is the files in raw that there's something to fetch for
// (not deleted, hidden, or kept elsewhere, like a Drive link).
func downloadable(raw jsontext.Value) []dlFile {
	var all []dlFile
	if len(raw) == 0 || jsonx.Unmarshal(raw, &all) != nil {
		return nil
	}
	var out []dlFile
	for _, f := range all {
		if f.Mode != "tombstone" && f.Mode != "hidden_by_limit" && (f.Direct != "" || f.Private != "") {
			out = append(out, f)
		}
	}
	return out
}

// save fetches the selected message's files.
func (m *Model) save(open bool) tea.Cmd {
	msg, ok := m.selected()
	if !ok {
		return m.say("pick a message first (↑)", false)
	}
	files := downloadable(msg.Files)
	if len(files) == 0 {
		return m.say("that message has no file to save", false)
	}
	api, ctx := m.api, m.ctx
	note := m.say("saving "+fileName(files[0])+"…", false)
	return tea.Batch(note, func() tea.Msg {
		dir, err := downloads()
		if err != nil {
			return savedMsg{err: err, open: open}
		}
		var paths []string
		for _, f := range files {
			out, err := create(dir, fileName(f))
			if err != nil {
				return savedMsg{paths, open, err}
			}
			_, err = api.Download(ctx, f.url(), out)
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				os.Remove(out.Name()) // half a file is worse than none
				return savedMsg{paths, open, fmt.Errorf("%s: %w", fileName(f), err)}
			}
			paths = append(paths, out.Name())
		}
		if open {
			for _, p := range paths {
				if err := openFile(p); err != nil {
					return savedMsg{paths, open, err}
				}
			}
		}
		return savedMsg{paths: paths, open: open}
	})
}

func (f dlFile) url() string {
	if f.Direct != "" {
		return f.Direct
	}
	return f.Private
}

// fileName is what a file is called on disk: its name, as a folder-free
// word.
func fileName(f dlFile) string {
	n := filepath.Base(strings.TrimSpace(f.Name))
	if n == "." || n == ".." || n == string(filepath.Separator) {
		return "file"
	}
	return n
}

// downloads is ~/Downloads, made if it isn't there.
func downloads() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Downloads")
	return dir, os.MkdirAll(dir, 0o755)
}

// create makes name in dir, or "name (1).ext", "name (2).ext" when there's
// already one: never over someone's file, whoever gets there first.
func create(dir, name string) (*os.File, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; ; i++ {
		n := name
		if i > 0 {
			n = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		f, err := os.OpenFile(filepath.Join(dir, n), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if !errors.Is(err, fs.ErrExist) {
			return f, err
		}
	}
}

// openFile hands path to the system to open. A variable, so tests don't.
var openFile = func(path string) error { return exec.Command("open", path).Run() }

// saved takes a save's end.
func (m *Model) saved(msg savedMsg) tea.Cmd {
	if msg.err != nil {
		return m.say("✗ couldn't save: "+msg.err.Error(), true)
	}
	names := make([]string, len(msg.paths))
	for i, p := range msg.paths {
		names[i] = filepath.Base(p)
	}
	text := "saved " + strings.Join(names, ", ") + " in Downloads"
	if msg.open {
		text = "opened " + strings.Join(names, ", ")
	}
	return m.say(text, false)
}
