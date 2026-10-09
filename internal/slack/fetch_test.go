package slack

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The cookie goes to Slack's own hosts and nowhere else.
func TestFetchCookieOnlyToSlack(t *testing.T) {
	var got []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Host+" "+r.Header.Get("Cookie"))
		w.Write([]byte("png"))
	}))
	defer srv.Close()
	c := New(Creds{Token: "xoxc-t", Cookie: "xoxd-c"})
	tr := srv.Client().Transport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	c.http = &http.Client{Transport: tr}
	for _, u := range []string{"https://files.slack.com/a.png", "https://avatars.slack-edge.com/a.png", "https://evilslack.com/a.png"} {
		if b, err := c.Fetch(context.Background(), u); err != nil || string(b) != "png" {
			t.Fatalf("%s: %q %v", u, b, err)
		}
	}
	if len(got) != 3 || got[0][:len("files.slack.com d=xoxd-c;")] != "files.slack.com d=xoxd-c;" || got[1] != "avatars.slack-edge.com " || got[2] != "evilslack.com " {
		t.Fatalf("%q", got)
	}
}
