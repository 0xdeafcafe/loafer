package ui

import (
	"cmp"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/0xdeafcafe/photon/fuzzy"
)

// match says what fuzzy.Match says, could never turns away what it'd
// match, and compareFold orders as lowering then comparing does.
func TestMatch(t *testing.T) {
	names := []string{"Ada Abbott", "ada.abbott0", "Zoë Ölund", "eng-payments", "İstanbul", "drew", "", "ÆSIR", "dev-ops"}
	queries := []string{"", "a", "ad", "ADA", "ab ad", "zo", "ölu", "eng pay", "pay eng", "x", "i", "æs", "-", "ops dev"}
	for _, s := range names {
		for _, q := range queries {
			s1, at1, ok1 := fuzzy.Match(q, s)
			s2, at2, ok2 := match(q, s)
			if s1 != s2 || ok1 != ok2 || !slices.Equal(at1, at2) {
				t.Errorf("match(%q, %q) = %d %v %v, fuzzy says %d %v %v", q, s, s2, at2, ok2, s1, at1, ok1)
			}
		}
		for _, o := range names {
			if got, want := compareFold(s, o), strings.Compare(strings.ToLower(s), strings.ToLower(o)); got != want {
				t.Errorf("compareFold(%q, %q) = %d, want %d", s, o, got, want)
			}
		}
	}
}

// top is the start of the sorted slice, whatever the order it's handed.
func TestTop(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for n := range 40 {
		s := make([]int, n)
		for i := range s {
			s[i] = rng.IntN(10)
		}
		want := slices.Sorted(slices.Values(s))
		for _, k := range []int{0, 1, 3, 8, 50} {
			got := top(slices.Clone(s), k, cmp.Compare[int])
			if !slices.Equal(got, want[:min(k, n)]) {
				t.Fatalf("top(%v, %d) = %v", s, k, got)
			}
		}
	}
}
