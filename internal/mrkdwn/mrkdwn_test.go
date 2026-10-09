package mrkdwn

import (
	"fmt"
	"strings"
	"testing"
)

// show writes lines compactly: [kind mark text target] per span.
func show(ls []Line) string {
	var b strings.Builder
	for _, l := range ls {
		if l.Pre {
			b.WriteString("PRE ")
		}
		if l.Quote {
			b.WriteString("> ")
		}
		for _, s := range l.Spans {
			fmt.Fprintf(&b, "[%d %d %q %q]", s.Kind, s.Mark, s.Text, s.Target)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestParse(t *testing.T) {
	for in, want := range map[string]string{
		"hi *there* _you_":                        "[0 0 \"hi \" \"\"][0 1 \"there\" \"\"][0 0 \" \" \"\"][0 2 \"you\" \"\"]\n",
		"2*3*4 snake_case_name":                   "[0 0 \"2*3*4 snake_case_name\" \"\"]\n",
		"see <https://x.io/a?b=1&amp;c|the docs>": "[0 0 \"see \" \"\"][2 0 \"the docs\" \"https://x.io/a?b=1&c\"]\n",
		"<@U1> and <#C1|dev> <!here>":             "[3 0 \"\" \"U1\"][0 0 \" and \" \"\"][4 0 \"dev\" \"C1\"][0 0 \" \" \"\"][6 0 \"@here\" \"here\"]\n",
		"run `go test` :tada: 10:30":              "[0 0 \"run \" \"\"][1 0 \"go test\" \"\"][0 0 \" \" \"\"][7 0 \"tada\" \"\"][0 0 \" 10:30\" \"\"]\n",
		":+1::skin-tone-3: :+1::skin-tone-7:":     "[7 0 \"+1::skin-tone-3\" \"\"][0 0 \" \" \"\"][7 0 \"+1\" \"\"][7 0 \"skin-tone-7\" \"\"]\n",
		"&gt; quoted &lt;b&gt;":                   "> [0 0 \"quoted <b>\" \"\"]\n",
		"before\n```\nx := 1\n```\nafter":         "[0 0 \"before\" \"\"]\nPRE [1 0 \"x := 1\" \"\"]\n[0 0 \"after\" \"\"]\n",
		"```one line```":                          "PRE [1 0 \"one line\" \"\"]\n",
		"<mailto:a@b.c>":                          "[2 0 \"a@b.c\" \"mailto:a@b.c\"]\n",
	} {
		if got := show(Parse(in)); got != want {
			t.Errorf("Parse(%q)\n got %s\nwant %s", in, got, want)
		}
	}
}

func TestMarkdown(t *testing.T) {
	for in, want := range map[string]string{
		"**bold** and __also__ ~~gone~~": "[0 1 \"bold\" \"\"][0 0 \" and \" \"\"][0 1 \"also\" \"\"][0 0 \" \" \"\"][0 4 \"gone\" \"\"]\n",
		"[the docs](https://x.io/a) ok":  "[2 0 \"the docs\" \"https://x.io/a\"][0 0 \" ok\" \"\"]\n",
		"[not](javascript:alert(1))":     "[0 0 \"[not](javascript:alert(1))\" \"\"]\n",
		"see https://x.io/a, then":       "[0 0 \"see \" \"\"][2 0 \"https://x.io/a\" \"https://x.io/a\"][0 0 \", then\" \"\"]\n",
	} {
		if got := show(Parse(in)); got != want {
			t.Errorf("Parse(%q)\n got %s\nwant %s", in, got, want)
		}
	}
}

func TestBlocks(t *testing.T) {
	ls := Parse("# Title\n- one\n  - nested\n- two\n1. first\n---\n| a | b |\n|---|:-:|\n| 1 | **2** |\n```go\nx := 1\n```\n* not a list*")
	var got []string
	for _, l := range ls {
		switch {
		case l.Heading > 0:
			got = append(got, fmt.Sprintf("h%d %s", l.Heading, l.Spans[0].Text))
		case l.Bullet != "":
			got = append(got, fmt.Sprintf("%s%d %s", l.Bullet, l.Depth, l.Spans[0].Text))
		case l.Rule:
			got = append(got, "rule")
		case l.Cells != nil:
			got = append(got, fmt.Sprintf("row%d head=%v %s", len(l.Cells), l.Head, l.Cells[1][0].Text))
		case l.Pre:
			got = append(got, fmt.Sprintf("pre %s start=%v %s", l.Lang, l.Start, l.Spans[0].Text))
		default:
			got = append(got, show([]Line{l}))
		}
	}
	want := []string{"h1 Title", "•0 one", "•1 nested", "•0 two", "1.0 first", "rule", "row2 head=true b", "row2 head=false 2", "pre go start=true x := 1", "•0 not a list*"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
}
