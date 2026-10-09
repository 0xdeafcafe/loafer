package store

// EachPerson calls f with everyone users.list has told us about, in no
// order. Like the rest of View, f gets pointers into the store.
func (v View) EachPerson(f func(*Person)) {
	for _, p := range v.s.people {
		f(p)
	}
}

// EachConv calls f with every conversation held, in no order.
func (v View) EachConv(f func(*Conv)) {
	for _, c := range v.s.convs {
		f(c)
	}
}
