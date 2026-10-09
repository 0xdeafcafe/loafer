package ui

import (
	"testing"
)

func TestTakeDraft(t *testing.T) {
	m := fixture(t)
	m.openConv("C1")
	done := make(chan error, 1)
	take := func(conv, thread, text string) error {
		m.update(Draft{Conv: conv, Thread: thread, Text: text, Done: done})
		return <-done
	}

	// The open conversation's box: after what's there, cursor left alone.
	m.insert("hi")
	m.cur = 1
	if err := take("C1", "", " from an agent \n"); err != nil || string(m.input) != "hi\nfrom an agent" || m.cur != 1 {
		t.Fatalf("open box: %q cur %d, %v", string(m.input), m.cur, err)
	}
	// Elsewhere: kept, and there on opening it.
	if err := take("C2", "", "for alerts"); err != nil {
		t.Fatal(err)
	}
	m.openConv("C2")
	if string(m.input) != "for alerts" {
		t.Fatalf("C2's box: %q", string(m.input))
	}
	// A thread not open: kept under the thread.
	if err := take("C1", "123.456", "in a thread"); err != nil || string(m.drafts["C1/123.456"].text) != "in a thread" {
		t.Fatalf("thread draft: %q, %v", string(m.drafts["C1/123.456"].text), err)
	}
	// Mid-edit, it's refused rather than mixed into the edit.
	m.editing = "1.000001"
	if err := take("C2", "", "no"); err == nil || string(m.input) != "for alerts" {
		t.Fatalf("mid-edit: %q, %v", string(m.input), err)
	}
	m.editing = ""
	if err := take("C2", "", "  "); err == nil {
		t.Fatal("an empty draft was taken")
	}
}
