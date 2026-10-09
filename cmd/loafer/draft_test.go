package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/ui"
)

func TestTakeDraft(t *testing.T) {
	var got ui.Draft
	send := func(m tea.Msg) {
		got = m.(ui.Draft)
		got.Done <- nil
	}
	if err := takeDraft(strings.NewReader(`{"team":"T1","conv":"C1","thread":"1.2","text":"hi"}`), []string{"T1"}, send); err != nil ||
		got.Team != "T1" || got.Conv != "C1" || got.Thread != "1.2" || got.Text != "hi" {
		t.Fatalf("%+v, %v", got, err)
	}
	var g ui.Goto
	if err := takeDraft(strings.NewReader(`{"open":true,"team":"T9","conv":"C1","ts":"1.3","thread":"1.2"}`), []string{"T1"}, func(m tea.Msg) { g = m.(ui.Goto) }); err != nil ||
		g != (ui.Goto{Team: "T9", Conv: "C1", TS: "1.3", Thread: "1.2"}) {
		t.Fatalf("goto %+v, %v", g, err)
	}
	for _, bad := range []string{`{"team":"T2","conv":"C1","text":"hi"}`, `{"team":"T1","text":"hi"}`, `not json`} {
		if err := takeDraft(strings.NewReader(bad), []string{"T1"}, func(tea.Msg) { t.Fatal("sent", bad) }); err == nil {
			t.Errorf("took %s", bad)
		}
	}
}
