package ui

import (
	"strings"
	"testing"

	"github.com/0xdeafcafe/loafer/internal/mrkdwn"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
)

const richSample = "## Deploy notes\n" +
	"Shipped **v2** to prod, see [the runbook](https://example.com/runbook).\n" +
	"- api: ok\n  - p99 went from 420ms to 180ms, which is nice and also a long line to wrap\n- worker: `restarted`\n" +
	"1. check graphs\n2. tell #ops\n" +
	"| service | before | after | owner |\n|---|---|---|---|\n| api | 420ms | 180ms | alex |\n| worker | 1.2s | 0.9s | drew |\n" +
	"```go\nfunc main() {\n\tfmt.Println(\"hi\") // say hi\n}\n```\n" +
	"---\n&gt; quoted *bit*"

func TestRichText(t *testing.T) {
	m := fixture(t)
	var rows []canvas.Row
	m.st.Read(func(v store.View) { rows = textRows(&m.pal, v, mrkdwn.Parse(richSample), 60) })
	for i, r := range rows {
		if r.Width() > 60 {
			t.Errorf("row %d is %d wide", i, r.Width())
		}
	}
	text := strings.Join(plainFrame(rows), "\n")
	if testing.Verbose() {
		t.Log("\n" + text)
	}
	for _, want := range []string{"Deploy notes", "• api: ok", "◦ p99", "1. check graphs", "service  before", "───", `fmt.Println("hi") // say hi`, " go ", "▏ quoted bit"} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q", want)
		}
	}
}

func BenchmarkRichText(b *testing.B) {
	m := fixture(b)
	lines := mrkdwn.Parse(richSample)
	m.st.Read(func(v store.View) {
		for b.Loop() {
			textRows(&m.pal, v, lines, 80)
		}
	})
}
