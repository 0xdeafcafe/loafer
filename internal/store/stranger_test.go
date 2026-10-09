package store

import (
	"context"
	"testing"
	"time"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/slacktest"
)

// Someone from another org users.list leaves out still has a name in
// your DMs, from users.info, once boot is done.
func TestBootMeetsStrangers(t *testing.T) {
	srv := slacktest.New()
	defer srv.Close()
	u := slack.User{ID: "UOUT", Name: "kim", RealName: "Kim Outside"}
	srv.Stranger(u, "DOUT")
	s := New()
	if err := s.Boot(context.Background(), srv.Client()); err != nil {
		t.Fatal(err)
	}
	var p Person
	for range 200 { // looked up after boot, beside it
		if s.Read(func(v View) { p = v.Person("UOUT") }); p.Real == "Kim Outside" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the stranger is %+v", p)
}
