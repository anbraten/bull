//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
)

func TestFileLifecycle(t *testing.T) {
	NewSuite(t, debianTarget).
		Case("create_then_noop_then_update", func(t *testing.T, h *harness) {
			remote := h.remotePath("config.txt")

			v1 := h.writeConfig(fmt.Sprintf(`
file("cfg", {
  host    = "h",
  path    = %q,
  content = "hello v1\n",
})
`, remote))

			if out, err := h.apply(v1, false); err != nil {
				t.Fatalf("create apply failed: %v\n%s", err, out)
			}
			if content, _ := h.exec("cat " + remote); content != "hello v1" {
				t.Fatalf("remote content = %q, want %q", content, "hello v1")
			}

			out, err := h.apply(v1, false)
			if err != nil {
				t.Fatalf("no-op apply failed: %v\n%s", err, out)
			}
			if !strings.Contains(out, "Nothing to apply") {
				t.Fatalf("expected a no-op apply, got:\n%s", out)
			}

			v2 := h.writeConfig(fmt.Sprintf(`
file("cfg", {
  host    = "h",
  path    = %q,
  content = "hello v2\n",
})
`, remote))

			if out, err := h.apply(v2, false); err != nil {
				t.Fatalf("update apply failed: %v\n%s", err, out)
			}
			if content, _ := h.exec("cat " + remote); content != "hello v2" {
				t.Fatalf("remote content after update = %q, want %q", content, "hello v2")
			}
		}).
		Run()
}
