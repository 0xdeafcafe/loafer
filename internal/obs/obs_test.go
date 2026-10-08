package obs

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	for in, want := range map[string]string{
		"token=xoxc-1234-abcd-5678":               "token=xoxc-REDACTED",
		"Cookie: d=xoxd-AbC%2Fdef; d-s=17":        "Cookie: d=xoxd-REDACTED; d-s=17",
		"Cookie: d=AbCdEfGhIjKlMnOpQrStUvWx; x=1": "Cookie: d=REDACTED; x=1",
		`{"token":"xoxb-1-2-3","ok":true}`:        `{"token":"REDACTED","ok":true}`,
		"no secrets, d=short":                     "no secrets, d=short",
	} {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTrace(t *testing.T) {
	defer Trace("")
	Trace("api, ws")
	if !Tracing(API) || !Tracing(WS) || Tracing(Render) {
		t.Fatal("api,ws should trace just those")
	}
	Trace("all")
	if !Tracing(Store) {
		t.Fatal("all should trace everything")
	}
}

// Lines reach the ring and the file redacted, and the file rotates.
func TestLogRingFileRotate(t *testing.T) {
	file := filepath.Join(t.TempDir(), "loafer.jsonl")
	stop := Start(file)
	for i := range ringSize + 10 {
		slog.Info("api", "method", "chat.postMessage", "i", i, "body", "token=xoxc-secret-1")
	}
	stop()
	got, n := Recent(3)
	if n != ringSize+10 || len(got) != 3 || !strings.Contains(got[2].Line, `"i":4105`) {
		t.Fatalf("ring: n=%d last=%+v", n, got)
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret") || !strings.Contains(string(b), "xoxc-REDACTED") {
		t.Fatal("file isn't redacted")
	}

	r := &rotator{path: filepath.Join(t.TempDir(), "r.log"), max: 10, keep: 2}
	for range 4 {
		r.Write([]byte("0123456789"))
	}
	r.Close()
	for _, name := range []string{r.path, r.path + ".1", r.path + ".2"} {
		if _, err := os.Stat(name); err != nil {
			t.Errorf("want %s: %v", name, err)
		}
	}
	if _, err := os.Stat(r.path + ".3"); err == nil {
		t.Error("kept more than keep")
	}
}
