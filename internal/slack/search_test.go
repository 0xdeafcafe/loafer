package slack

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.URL.Path != "/api/search.messages" || r.Form.Get("query") != "deploy in:<#C1>" || r.Form.Get("page") != "2" || r.Form.Get("highlight") != "true" {
			io.WriteString(w, `{"ok":false,"error":"bad_request"}`)
			return
		}
		// Only pagination, as some answers have it.
		io.WriteString(w, `{"ok":true,"messages":{"total":41,"pagination":{"page":2,"page_count":3},"matches":[
			{"channel":{"id":"C1","name":"dev"},"user":"U1","ts":"2.0","text":"deploy it","permalink":"https://x.slack.com/archives/C1/p20?thread_ts=1.0&cid=C1"},
			{"channel":{"id":"D1","name":"U2","is_im":true},"user":"U2","ts":"3.0","text":"deploy","permalink":"https://x.slack.com/archives/D1/p30"}]}}`)
	}))
	defer srv.Close()
	f, err := New(Creds{URL: srv.URL, Token: "xoxc-t"}).Search(context.Background(), "deploy in:<#C1>", 2)
	if err != nil || f.Page != 2 || f.Pages != 3 || f.Total != 41 || len(f.Matches) != 2 {
		t.Fatalf("%+v %v", f, err)
	}
	if p := f.Matches[0].Parent(); p != "1.0" {
		t.Errorf("a reply's parent: %q", p)
	}
	if p := f.Matches[1].Parent(); p != "" || !f.Matches[1].Channel.IsIM {
		t.Errorf("not a reply: %q", p)
	}
}
