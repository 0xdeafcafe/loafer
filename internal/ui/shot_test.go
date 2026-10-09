package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/0xdeafcafe/loafer/internal/slacktest"
)

// TestShot prints the demo workspace's Home with #dev open at a few sizes,
// for looking at the layout: LOAFER_SHOT=1 go test -run TestShot -v.
func TestShot(t *testing.T) {
	if os.Getenv("LOAFER_SHOT") == "" {
		t.Skip("set LOAFER_SHOT=1")
	}
	d := newE2E(t)
	d.jump("dev", slacktest.Dev)
	d.until("#dev's messages", func() bool { return d.has("deployed loafer@4f2a9c1") })
	for _, size := range [][2]int{{160, 45}, {90, 30}} {
		d.m.w, d.m.h = size[0], size[1]
		t.Logf("%dx%d\n%s", size[0], size[1], strings.Join(plainFrame(d.m.render()), "\n"))
	}
}
