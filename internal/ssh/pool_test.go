package ssh

import (
	"os/exec"
	"strings"
	"testing"
)

func shellEcho(t *testing.T, s string) string {
	t.Helper()
	out, err := exec.Command("sh", "-c", "printf %s "+ShellQuote(s)).Output()
	if err != nil {
		t.Fatalf("sh failed for %q: %v", s, err)
	}
	return string(out)
}

func TestShellQuoteRoundTrip(t *testing.T) {
	cases := []string{
		"", "plain", "with space", "it's", "''", `"double"`, "$HOME", "`id`",
		`back\slash`, "$(echo pwned)", "new\nline", "tab\there", "*", "a;b|c&d",
	}
	for _, c := range cases {
		if got := shellEcho(t, c); got != c {
			t.Errorf("ShellQuote(%q) round-tripped to %q", c, got)
		}
	}
}

func FuzzShellQuote(f *testing.F) {
	for _, s := range []string{"", "it's", "$HOME", "`id`", `a\b`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if strings.ContainsRune(s, 0) {
			t.Skip("NUL can't be passed through argv")
		}
		if got := shellEcho(t, s); got != s {
			t.Errorf("ShellQuote(%q) round-tripped to %q", s, got)
		}
	})
}
