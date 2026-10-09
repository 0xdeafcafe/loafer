package notify

import (
	"encoding/json/jsontext"
	"testing"
)

func TestMute(t *testing.T) {
	var p Prefs
	if v := p.Mute("C2", true); v != "C2" {
		t.Fatalf("mute: %q", v)
	}
	if v := p.Mute("C1", true); v != "C1,C2" {
		t.Fatalf("sorted: %q", v)
	}
	if !p.Muted("C1") || p.Muted("C3") {
		t.Fatal("muted wrong")
	}
	// Muted by the per-conversation settings too, until unmuted.
	p.Set("all_notifications_prefs", jsontext.Value(`"{\"channels\":{\"C3\":{\"muted\":true}}}"`))
	if !p.Muted("C3") {
		t.Fatal("C3 should be muted by its settings")
	}
	if v := p.Mute("C3", false); v != "C1,C2" || p.Muted("C3") {
		t.Fatalf("unmute: %q, muted %v", v, p.Muted("C3"))
	}
}
