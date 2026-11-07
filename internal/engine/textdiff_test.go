package engine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/fatih/color"
)

func opsString(ops []lineOp) string {
	var b strings.Builder
	for _, op := range ops {
		b.WriteByte(byte(op.kind))
		b.WriteString(op.text)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestLineDiff(t *testing.T) {
	tests := []struct {
		name, a, b, want string
	}{
		{"equal", "a\nb\n", "a\nb\n", " a\n b\n"},
		{"replace middle", "a\nb\nc\n", "a\nx\nc\n", " a\n-b\n+x\n c\n"},
		{"insert", "a\nc\n", "a\nb\nc\n", " a\n+b\n c\n"},
		{"delete", "a\nb\nc\n", "a\nc\n", " a\n-b\n c\n"},
		{"from empty", "", "a\n", "+a\n"},
		{"to empty", "a\n", "", "-a\n"},
		{"interleaved", "a\nb\nc\nd\n", "b\nc\nx\nd\n", "-a\n b\n c\n+x\n d\n"},
	}
	for _, tt := range tests {
		got := opsString(lineDiff(splitLines(tt.a), splitLines(tt.b)))
		if got != tt.want {
			t.Errorf("%s: got\n%s\nwant\n%s", tt.name, got, tt.want)
		}
	}
}

func TestHunks(t *testing.T) {
	var a, b []string
	for i := 1; i <= 20; i++ {
		a = append(a, string(rune('a'+i-1)))
	}
	b = append(b, a...)
	b[1] = "X"  // line 2
	b[17] = "Y" // line 18: far enough away for a second hunk

	hs := hunks(lineDiff(a, b))
	if len(hs) != 2 {
		t.Fatalf("got %d hunks, want 2", len(hs))
	}
	var headers []string
	for _, h := range hs {
		headers = append(headers, h.header())
	}
	want := []string{"@@ -1,5 +1,5 @@", "@@ -15,6 +15,6 @@"}
	if !reflect.DeepEqual(headers, want) {
		t.Errorf("headers = %v, want %v", headers, want)
	}

	// Changes within 2*context lines merge into one hunk.
	b = append([]string{}, a...)
	b[1], b[7] = "X", "Y"
	if hs := hunks(lineDiff(a, b)); len(hs) != 1 {
		t.Errorf("close changes: got %d hunks, want 1", len(hs))
	}

	if hs := hunks(lineDiff(a, a)); hs != nil {
		t.Errorf("no changes: got %d hunks", len(hs))
	}
}

func TestHunkEmptyRange(t *testing.T) {
	hs := hunks(lineDiff(nil, []string{"a"}))
	if len(hs) != 1 || hs[0].header() != "@@ -0,0 +1,1 @@" {
		t.Errorf("got %+v", hs)
	}
}

func TestRedactSecrets(t *testing.T) {
	secrets := map[string]string{"short": "pass", "long": "password123", "empty": ""}
	got := redactSecrets("DB_PASSWORD=password123 OTHER=pass", secrets)
	want := "DB_PASSWORD=[REDACTED] OTHER=[REDACTED]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTextDiffLines(t *testing.T) {
	color.NoColor = true
	defer func() { color.NoColor = false }()
	secrets := map[string]string{"pw": "hunter2"}

	summary, body := textDiffLines(Diff{
		Before: "image: app:1.0\npassword: old\n",
		After:  "image: app:1.1\npassword: hunter2\n",
		Text:   true,
	}, ChangeUpdate, secrets, false)
	if summary != "+2 -2" {
		t.Errorf("summary = %q", summary)
	}
	want := []string{"@@ -1,2 +1,2 @@", "-image: app:1.0", "-password: old", "+image: app:1.1", "+password: [REDACTED]"}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %q, want %q", body, want)
	}

	summary, body = textDiffLines(Diff{After: strings.Repeat("l\n", maxCreatePreview+5), Text: true}, ChangeCreate, nil, false)
	if summary != "+35 lines" || len(body) != maxCreatePreview+1 || body[len(body)-1] != "… 5 more lines (--full-diff to show)" {
		t.Errorf("create preview: summary=%q len=%d last=%q", summary, len(body), body[len(body)-1])
	}

	summary, body = textDiffLines(Diff{Before: "a", After: "a\n", Text: true}, ChangeUpdate, nil, false)
	if summary != "trailing newline changed" || body != nil {
		t.Errorf("newline-only: %q %q", summary, body)
	}

	summary, _ = textDiffLines(Diff{Before: "a\x00", After: "b\x00c", Text: true}, ChangeUpdate, nil, false)
	if summary != "binary, 2 → 3 bytes" {
		t.Errorf("binary: %q", summary)
	}
}

func TestTextDiffLinesFull(t *testing.T) {
	color.NoColor = true
	defer func() { color.NoColor = false }()
	_, body := textDiffLines(Diff{After: strings.Repeat("l\n", maxCreatePreview+5), Text: true}, ChangeCreate, nil, true)
	if len(body) != maxCreatePreview+5 {
		t.Errorf("full create preview: got %d lines, want %d", len(body), maxCreatePreview+5)
	}
}
