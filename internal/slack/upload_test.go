package slack

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Upload asks for a place, puts the bytes there with no cookie, and shares
// them; progress ends at the total.
func TestUpload(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	var form map[string]string
	var body []byte
	var length int64
	var cookie string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/api/files.getUploadURLExternal":
			_ = r.ParseForm()
			if r.PostForm.Get("filename") != "a.bin" || r.PostForm.Get("length") != "9" {
				t.Errorf("asked for %v", r.PostForm)
			}
			w.Write([]byte(`{"ok":true,"upload_url":"` + srv.URL + `/up/1","file_id":"F1"}`))
		case "/up/1":
			body, _ = io.ReadAll(r.Body)
			length, cookie = r.ContentLength, r.Header.Get("Cookie")
			w.Write([]byte("OK - 9"))
		case "/api/files.completeUploadExternal":
			_ = r.ParseForm()
			form = map[string]string{}
			for k := range r.PostForm {
				form[k] = r.PostForm.Get(k)
			}
			w.Write([]byte(`{"ok":true,"files":[{"id":"F1"}]}`))
		}
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "a.bin")
	if err := os.WriteFile(path, []byte("nine byte"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New(Creds{URL: srv.URL + "/", Token: "xoxc-t", Cookie: "xoxd-c"})
	var sent, total int64
	err := c.Upload(context.Background(), Share{Channel: "C1", ThreadTS: "1.2", Text: "here", Also: true},
		[]UploadFile{{Path: path, Name: "a.bin", Size: 9}}, func(s, n int64) { sent, total = s, n })
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(calls, " "); got != "/api/files.getUploadURLExternal /up/1 /api/files.completeUploadExternal" {
		t.Errorf("calls: %s", got)
	}
	if !bytes.Equal(body, []byte("nine byte")) || length != 9 || cookie != "" {
		t.Errorf("body %q, length %d, cookie %q", body, length, cookie)
	}
	if sent != 9 || total != 9 {
		t.Errorf("progress %d of %d", sent, total)
	}
	want := map[string]string{"channel_id": "C1", "thread_ts": "1.2", "reply_broadcast": "true", "initial_comment": "here",
		"files": `[{"id":"F1","title":"a.bin"}]`, "token": "xoxc-t"}
	for k, v := range want {
		if form[k] != v {
			t.Errorf("complete %s = %q, want %q", k, form[k], v)
		}
	}
}

// A failed put is an error, and nothing is shared.
func TestUploadRefused(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/files.getUploadURLExternal":
			w.Write([]byte(`{"ok":true,"upload_url":"` + srv.URL + `/up/1","file_id":"F1"}`))
		case "/up/1":
			http.Error(w, "no", http.StatusForbidden)
		default:
			t.Errorf("went on to %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "a.bin")
	_ = os.WriteFile(path, []byte("x"), 0o600)
	c := New(Creds{URL: srv.URL + "/", Token: "xoxc-t"})
	err := c.Upload(context.Background(), Share{Channel: "C1"}, []UploadFile{{Path: path, Name: "a.bin", Size: 1}}, func(int64, int64) {})
	if e, ok := err.(*Error); !ok || e.Status != http.StatusForbidden {
		t.Fatalf("err %v", err)
	}
}

func TestDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			http.NotFound(w, r)
			return
		}
		w.Write(bytes.Repeat([]byte("x"), 100))
	}))
	defer srv.Close()
	c := New(Creds{Token: "xoxc-t"})
	var b bytes.Buffer
	if n, err := c.Download(context.Background(), srv.URL+"/f", &b); err != nil || n != 100 || b.Len() != 100 {
		t.Fatalf("%d %d %v", n, b.Len(), err)
	}
	if _, err := c.Download(context.Background(), srv.URL+"/gone", &b); err == nil {
		t.Fatal("a 404 saved")
	}
}
