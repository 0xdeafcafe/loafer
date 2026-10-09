package emoji

import (
	"testing"

	"github.com/0xdeafcafe/photon/cellw"
)

func TestLookup(t *testing.T) {
	for name, want := range map[string]string{
		"+1":                    "👍",
		"thumbsup":              "👍",
		"smile":                 "😄",
		"heart":                 "❤️",
		"+1::skin-tone-2":       "👍🏻",
		"thumbsup::skin-tone-6": "👍🏿",
	} {
		if got, ok := Lookup(name); !ok || got != want {
			t.Errorf("%q: %q %v, want %q", name, got, ok, want)
		}
	}
	for _, name := range []string{"", "nope", "+1::skin-tone-7", "+1::skin-tone-", "smile::skin-tone-3", "custom_thing"} {
		if got, ok := Lookup(name); ok {
			t.Errorf("%q: %q, want none", name, got)
		}
	}
}

func TestCanon(t *testing.T) {
	for in, want := range map[string]string{"thumbsup": "+1", "+1": "+1", "thumbsup::skin-tone-3": "+1::skin-tone-3", "custom": "custom"} {
		if got := Canon(in); got != want {
			t.Errorf("Canon(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNames(t *testing.T) {
	ns := Names()
	if len(ns) < 1800 {
		t.Fatalf("%d names", len(ns))
	}
	for _, n := range ns {
		if c, ok := Lookup(n); !ok || cellw.String(c) < 1 || cellw.String(c) > 2 {
			t.Fatalf("%q: %q is %d cells", n, c, cellw.String(c))
		}
	}
}

func TestGroups(t *testing.T) {
	gs := Groups()
	n := 0
	for _, g := range gs {
		n += len(g.Names)
	}
	if len(gs) != 9 || gs[0].Name != "Smileys & Emotion" || gs[0].Names[0] != "grinning" || gs[8].Name != "Flags" || n < 1800 {
		t.Fatalf("%d groups, %d emoji: %+v", len(gs), n, gs[0].Name)
	}
}

func BenchmarkLookup(b *testing.B) {
	Lookup("smile")
	for b.Loop() {
		Lookup("thumbsup")
	}
}
