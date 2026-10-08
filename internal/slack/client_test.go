package slack

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCallSendsCredsAndReadsErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f, _ := url.ParseQuery(string(b))
		c, _ := r.Cookie("d")
		switch {
		case f.Get("token") != "xoxc-t" || c == nil || c.Value != "xoxd-c%2Fx":
			io.WriteString(w, `{"ok":false,"error":"invalid_auth"}`)
		case r.URL.Path == "/api/auth.test":
			io.WriteString(w, `{"ok":true,"url":"https://lw.slack.com/","team":"LangWatch","team_id":"T1","user_id":"U1","extra":1}`)
		default:
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}))
	defer srv.Close()

	c := New(Creds{URL: srv.URL, Token: "xoxc-t", Cookie: "xoxd-c%2Fx"})
	got, err := c.AuthTest(context.Background())
	if err != nil || got.TeamID != "T1" || got.URL != "https://lw.slack.com/" || got.Token != "xoxc-t" {
		t.Fatalf("auth.test: %+v %v", got, err)
	}
	err = c.Call(context.Background(), "chat.postMessage", nil, nil)
	if e, ok := err.(*Error); !ok || e.Status != 429 || e.RetryAfter.Seconds() != 3 {
		t.Fatalf("429: %v", err)
	}
	_, err = New(Creds{URL: srv.URL, Token: "bad"}).AuthTest(context.Background())
	if !SignedOut(err) {
		t.Fatalf("want signed out, got %v", err)
	}
}

func TestClean(t *testing.T) {
	if got := CleanCookie(` d=xoxd-a/b+c=; `); got != "xoxd-a%2Fb%2Bc%3D" {
		t.Errorf("decoded cookie: %q", got)
	}
	if got := CleanCookie(`"xoxd-a%2Fb"`); got != "xoxd-a%2Fb" {
		t.Errorf("encoded cookie: %q", got)
	}
	if got := CleanToken(` "xoxc-1-2" `); got != "xoxc-1-2" || strings.Contains(got, `"`) {
		t.Errorf("token: %q", got)
	}
}

// An app's token goes as a bearer, with no cookie and no token field.
func TestBearerForAppTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer xoxb-1" || r.Header.Get("Cookie") != "" || strings.Contains(string(b), "token") {
			io.WriteString(w, `{"ok":false,"error":"not_authed"}`)
			return
		}
		io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	if err := New(Creds{URL: srv.URL, Token: "xoxb-1"}).Call(context.Background(), "chat.postMessage", nil, nil); err != nil {
		t.Fatal(err)
	}
}
