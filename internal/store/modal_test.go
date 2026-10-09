package store

import "testing"

// A modal opens only for a token sent from here; pushes stack on it,
// updates replace, and closing takes it and what's on it away.
func TestModal(t *testing.T) {
	s := New()
	top := func() (id string) {
		s.Read(func(v View) {
			if m := v.Modal(); m != nil {
				id = m.ID
			}
		})
		return id
	}

	s.Apply(ev(t, `{"type":"view_opened","client_token":"web-1","view_type":"modal","view_id":"V1","view":{"id":"V1","type":"modal"}}`))
	if top() != "" {
		t.Fatal("opened without being asked for")
	}
	s.ExpectView("web-1")
	s.Apply(ev(t, `{"type":"view_opened","client_token":"web-1","view_type":"home","view_id":"V0","view":{"type":"home"}}`))
	if top() != "" {
		t.Fatal("a home tab isn't a modal")
	}
	// The view as a string of JSON, in case it comes that way.
	s.Apply(ev(t, `{"type":"view_opened","client_token":"web-1","view_id":"V1","view":"{\"id\":\"V1\",\"type\":\"modal\",\"hash\":\"h1\"}"}`))
	if top() != "V1" {
		t.Fatalf("not opened: %q", top())
	}
	s.Apply(ev(t, `{"type":"view_pushed","previous_view_id":"V1","view":{"id":"V2","type":"modal"}}`))
	s.Apply(ev(t, `{"type":"view_updated","view_id":"V1","view":{"id":"V1","type":"modal","hash":"h2"}}`))
	s.ModalErrors("V2", map[string]string{"b": "no"})
	s.Read(func(v View) {
		if m := v.Modal(); m.ID != "V2" || m.Errors["b"] != "no" || v.s.md.stack[0].Hash != "h2" {
			t.Fatalf("stack: %+v %+v", m, v.s.md.stack[0])
		}
	})
	s.Apply(ev(t, `{"type":"view_closed","view_id":"V1"}`))
	if top() != "" {
		t.Fatalf("closing V1 leaves %q", top())
	}
}
