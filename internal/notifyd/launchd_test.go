package notifyd

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// fakeLaunchd stands in for launchctl, keeping what it was asked and
// whether notifyd is loaded.
type fakeLaunchd struct {
	loaded bool
	ran    []string
}

func (f *fakeLaunchd) run(args ...string) error {
	f.ran = append(f.ran, args[0])
	switch args[0] {
	case "print":
		if !f.loaded {
			return errors.New("not loaded")
		}
	case "bootstrap":
		f.loaded = true
	case "bootout":
		f.loaded = false
	}
	return nil
}

func TestAgent(t *testing.T) {
	f := &fakeLaunchd{}
	a := Agent{Dir: t.TempDir(), Run: f.run}
	check := func(what string, want ...string) {
		t.Helper()
		if got := strings.Join(f.ran, " "); got != strings.Join(want, " ") {
			t.Fatalf("%s ran %q, want %q", what, got, want)
		}
		f.ran = nil
	}

	if err := a.Install("/bin/loafer & co"); err != nil {
		t.Fatal(err)
	}
	check("install", "print", "bootstrap")
	b, _ := os.ReadFile(a.Path())
	if !strings.Contains(string(b), "<string>/bin/loafer &amp; co</string>") || !strings.Contains(string(b), Label) {
		t.Fatalf("plist:\n%s", b)
	}

	if err := a.Install("/bin/loafer & co"); err != nil || !f.loaded {
		t.Fatal(err)
	}
	check("install again", "print", "kickstart")

	if err := a.Install("/usr/local/bin/loafer"); err != nil || !f.loaded {
		t.Fatal(err)
	}
	check("install elsewhere", "print", "bootout", "bootstrap")

	if err := a.Uninstall(); err != nil || f.loaded || a.Installed() {
		t.Fatalf("uninstall: %v, loaded %v", err, f.loaded)
	}
	check("uninstall", "print", "bootout")
	if err := a.Uninstall(); err != nil {
		t.Fatal(err)
	}
	check("uninstall again", "print")
}
