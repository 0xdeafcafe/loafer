package store

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/slacktest"
	"github.com/0xdeafcafe/photon/jsonx"
)

// The store at the big workspace's size (slacktest.NewBig): booted, with
// twenty conversations' windows held. docs/perf.md has the numbers.

func bigStore(b *testing.B) (*Store, *slacktest.Server) {
	srv := slacktest.NewBig()
	b.Cleanup(srv.Close)
	s, c := New(), srv.Client()
	ctx := context.Background()
	if err := s.Boot(ctx, c); err != nil {
		b.Fatal(err)
	}
	var ids []string
	s.Read(func(v View) {
		for _, sec := range v.Sidebar() {
			ids = append(ids, sec.Convs...)
		}
	})
	for _, id := range ids[:20] {
		if err := s.Open(ctx, c, id); err != nil {
			b.Fatal(err)
		}
	}
	return s, srv
}

// cpu reports the process's CPU time an op beside the wall time, which
// a busy machine stretches.
func cpu(b *testing.B) func() {
	at := cpuTime()
	return func() { b.ReportMetric(float64(cpuTime()-at)/float64(b.N), "cpu-ns/op") }
}

func cpuTime() time.Duration {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

func BenchmarkBigSidebar(b *testing.B) {
	s, _ := bigStore(b)
	defer cpu(b)()
	for b.Loop() {
		s.Read(func(v View) { v.Sidebar() })
	}
}

func BenchmarkBigSave(b *testing.B) {
	s, _ := bigStore(b)
	path := filepath.Join(b.TempDir(), "state.json")
	defer cpu(b)()
	for b.Loop() {
		if err := s.Save(path); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBigLoad(b *testing.B) {
	s, _ := bigStore(b)
	path := filepath.Join(b.TempDir(), "state.json")
	if err := s.Save(path); err != nil {
		b.Fatal(err)
	}
	defer cpu(b)()
	for b.Loop() {
		if err := New().Load(path); err != nil {
			b.Fatal(err)
		}
	}
}

// A page of users.list, decoded as slack.Client.Users decodes it, and
// applied.
func BenchmarkBigPeoplePage(b *testing.B) {
	s, srv := bigStore(b)
	resp, err := http.PostForm(srv.URL+"/api/users.list", url.Values{"limit": {"500"}, "cursor": {"1000"}})
	if err != nil {
		b.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	b.SetBytes(int64(len(body)))
	defer cpu(b)()
	for b.Loop() {
		var r struct {
			Members []slack.User `json:"members"`
		}
		if err := jsonx.Unmarshal(body, &r); err != nil {
			b.Fatal(err)
		}
		s.ApplyPeople(r.Members)
	}
}

// One websocket message event, as Live hands it to Apply.
func BenchmarkBigApplyMessage(b *testing.B) {
	s, _ := bigStore(b)
	defer cpu(b)()
	i := 0
	for b.Loop() {
		i++
		raw := fmt.Appendf(nil, `{"type":"message","channel":%q,"user":"UB00042","text":"on it","ts":"%d.000100"}`, slacktest.Big, 2000000000+i)
		s.Apply(slack.Event{Type: "message", Raw: raw})
	}
}
