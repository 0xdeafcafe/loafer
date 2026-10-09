package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slacktest"
	"github.com/0xdeafcafe/loafer/internal/store"
)

// coldE2E starts a model on st as loafer does (welcome if cold), with
// methods held until the test lets them go, and doesn't wait for boot.
func coldE2E(t *testing.T, st *store.Store, held ...string) (*e2e, map[string]func()) {
	srv := slacktest.New()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); srv.Close() })
	release := map[string]func(){}
	for _, h := range held {
		release[h] = srv.Hold(h)
	}
	d := &e2e{t: t, srv: srv, ctx: ctx, msgs: make(chan tea.Msg, 64)}
	d.m = New(ctx, st, srv.Client())
	d.m.w, d.m.h = 120, 40
	d.m.WelcomeIfCold(false)
	d.run(d.m.Init())
	return d, release
}

func TestE2EWelcome(t *testing.T) {
	d, release := coldE2E(t, store.New(), "users.list", "emoji.list")
	if !d.m.wel.on {
		t.Fatal("a cold store should show the welcome")
	}
	d.until("the workspace in, people still coming", func() bool {
		return d.has("welcome to loafer, sam") && d.has("✓ the workspace") && d.has("✓ unread counts") &&
			d.has("✓ channels and sections") && d.has("◌ people") && d.has("◌ emoji") && d.has("any key to start")
	})
	if !d.has("Crumb & Co is new to loafer") || !d.has("ctrl+k") {
		t.Fatalf("the hello or the keys are missing:\n%s", d.text)
	}
	release["users.list"]()
	d.until("people in", func() bool { return d.has("✓ people") })
	if !d.m.wel.on {
		t.Fatal("handed over with emoji still on its way")
	}
	release["emoji.list"]()
	d.until("the app", func() bool { return !d.m.wel.on && d.has("● live") && d.m.open != "" })
	if d.has("welcome to") || !d.has("# general") {
		t.Fatalf("not the app:\n%s", d.text)
	}
}

func TestE2EWelcomeSkip(t *testing.T) {
	d, release := coldE2E(t, store.New(), "users.list", "client.counts")
	defer release["users.list"]()
	d.until("the workspace", func() bool { return d.has("✓ the workspace") })
	d.press(r('x'))
	if !d.m.wel.on {
		t.Fatal("a key skipped ahead before the sidebar could draw")
	}
	release["client.counts"]()
	d.until("ready", func() bool { return d.has("any key to start") })
	d.press(r('x'))
	if d.m.wel.on {
		t.Fatal("a key should go to the app once it's ready")
	}
	d.until("the app", func() bool { return d.has("# general") && d.m.open != "" })
}

func TestE2EWelcomeSignedOut(t *testing.T) {
	d, _ := coldE2E(t, store.New())
	d.srv.SetSignedOut(true)
	d.until("the sign-in refused", func() bool { return d.has("slack signed you out") })
	d.press(r('x'))
	if !d.m.SignedOut() {
		t.Fatal("signed out, a key should quit to sign in again")
	}
}

func TestE2EWelcomeWarm(t *testing.T) {
	// Boot once, save the cache, and start again from it.
	path := filepath.Join(t.TempDir(), "state.json")
	first := newE2E(t)
	if err := first.m.st.Save(path); err != nil {
		t.Fatal(err)
	}
	st := store.New()
	if err := st.Load(path); err != nil {
		t.Fatal(err)
	}
	d, _ := coldE2E(t, st)
	if d.m.wel.on {
		t.Fatal("a warm cache showed the welcome")
	}
	d.until("the app", func() bool {
		if d.has("welcome to") {
			t.Fatalf("welcome drawn over a warm cache:\n%s", d.text)
		}
		return d.has("● live") && d.m.open != ""
	})
}

// TestWelcomeSizes draws the welcome at the sizes it must fit, and writes
// them out with LOAFER_WELCOME_DUMP set.
func TestWelcomeSizes(t *testing.T) {
	d, release := coldE2E(t, store.New(), "users.list")
	defer release["users.list"]()
	d.until("ready", func() bool { return d.has("any key to start") })
	d.m.wel.lace = laceN
	var dump strings.Builder
	for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 20}, {40, 12}, {30, 8}} {
		d.m.w, d.m.h = size[0], size[1]
		rows := d.m.render()
		if len(rows) != d.m.h {
			t.Fatalf("%v: %d rows", size, len(rows))
		}
		for i, row := range rows {
			if row.Width() != d.m.w {
				t.Fatalf("%v: row %d is %d wide", size, i, row.Width())
			}
		}
		text := strings.Join(plainFrame(rows), "\n")
		if !strings.Contains(text, "welcome to") || !strings.Contains(text, "people") {
			t.Fatalf("%v lost the essentials:\n%s", size, text)
		}
		dump.WriteString(text + "\n\n")
	}
	if p := os.Getenv("LOAFER_WELCOME_DUMP"); p != "" {
		_ = os.WriteFile(p, []byte(dump.String()), 0o644)
	}
}
