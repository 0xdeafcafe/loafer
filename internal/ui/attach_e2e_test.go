package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xdeafcafe/loafer/internal/slacktest"
)

// Attach a file through the prompt, send it with a comment, watch it
// appear, save it, open it, then do it again in a thread.
func TestE2EFilesInAndOut(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var opened []string
	was := openFile
	openFile = func(p string) error { opened = append(opened, p); return nil }
	t.Cleanup(func() { openFile = was })

	src := write(t, t.TempDir(), "notes.txt", "fresh bread")
	d := newE2E(t)
	d.jump("general", slacktest.General)
	d.until("#general", func() bool { return d.has("projector cable") })

	attach := func(path string) {
		d.press(cCtrlO)
		d.press(cBackspace, cBackspace) // the ~/ it starts with
		d.typed(path)
		d.press(cEnter)
	}
	attach(src)
	d.until("the chip", func() bool { return d.has("▤ notes.txt · 11 B") })
	d.typed("the notes")
	d.press(cEnter)
	d.until("the upload", func() bool {
		return d.m.att.up == nil && len(d.m.att.files) == 0 && d.has("the notes") && d.has("▤ notes.txt")
	})
	if got, ok := d.srv.File("notes.txt"); !ok || string(got) != "fresh bread" {
		t.Fatalf("Slack has %q", got)
	}
	if len(d.m.input) != 0 {
		t.Fatalf("the box kept %q", string(d.m.input))
	}

	// The message is the newest: D saves it, twice over to see the clash,
	// and O opens it.
	d.press(esc)
	for range 2 {
		d.press(r('D'))
	}
	d.until("both saves", func() bool {
		_, err := os.Stat(filepath.Join(home, "Downloads", "notes (1).txt"))
		return err == nil && d.has("saved notes (1).txt")
	})
	for _, name := range []string{"notes.txt", "notes (1).txt"} {
		if b, err := os.ReadFile(filepath.Join(home, "Downloads", name)); err != nil || string(b) != "fresh bread" {
			t.Fatalf("%s: %q %v", name, b, err)
		}
	}
	d.press(r('O'))
	d.until("opened", func() bool { return d.has("opened notes (2).txt") })
	if len(opened) != 1 || filepath.Base(opened[0]) != "notes (2).txt" {
		t.Fatalf("opened %q", opened)
	}

	// In a thread, from the thread's box: a reply, not a post.
	d.press(r('t'))
	if d.m.th.ts == "" {
		t.Fatalf("no thread:\n%s", d.text)
	}
	parent := d.m.th.ts
	attach(src)
	d.until("the thread's chip", func() bool { return d.has("▤ notes.txt") && len(d.m.att.files[slacktest.General+"/"+parent]) == 1 })
	d.press(cEnter)
	d.until("the reply", func() bool { return d.m.att.up == nil && len(d.m.att.files) == 0 })
	d.until("it's in the thread", func() bool {
		for _, c := range d.srv.Calls() {
			if c.Method == "files.completeUploadExternal" && c.Form.Get("thread_ts") == parent && c.Form.Get("channel_id") == slacktest.General {
				return true
			}
		}
		return false
	})
}

// A file that Slack won't take goes back in the box with its words.
func TestE2EUploadFails(t *testing.T) {
	src := write(t, t.TempDir(), "a.txt", "x")
	d := newE2E(t)
	d.jump("general", slacktest.General)
	d.until("#general", func() bool { return d.has("projector cable") })
	d.press(cCtrlO, cBackspace, cBackspace)
	d.typed(src)
	d.press(cEnter)
	d.until("the chip", func() bool { return d.has("▤ a.txt") })
	d.typed("look")
	if err := os.Remove(src); err != nil { // gone before it's sent
		t.Fatal(err)
	}
	d.press(cEnter)
	d.until("the failure", func() bool { return d.m.att.up == nil && d.has("couldn't upload") })
	if len(d.m.att.files[slacktest.General]) != 1 || string(d.m.input) != "look" {
		t.Fatalf("not put back: %+v %q", d.m.att.files, string(d.m.input))
	}
}
