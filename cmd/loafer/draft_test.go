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
	if err := takeDraft(strings.NewReader(`{"team":"T1","conv":"C1","thread":"1.2","text":"hi"}`), "T1", send); err != nil ||
		got.Conv != "C1" || got.Thread != "1.2" || got.Text != "hi" {
		t.Fatalf("%+v, %v", got, err)
	}
	for _, bad := range []string{`{"team":"T2","conv":"C1","text":"hi"}`, `{"team":"T1","text":"hi"}`, `not json`} {
		if err := takeDraft(strings.NewReader(bad), "T1", func(tea.Msg) { t.Fatal("sent", bad) }); err == nil {
			t.Errorf("took %s", bad)
		}
	}
}
