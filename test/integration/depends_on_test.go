//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
)

func TestDependsOn(t *testing.T) {
	NewSuite(t, debianTarget).
		Case("skips_dependent_when_dependency_fails", func(t *testing.T, h *harness) {
			marker := h.remotePath("marker")

			cfg := h.writeConfig(fmt.Sprintf(`
shell("boom", {
  host    = "h",
  command = "exit 1",
})

shell("dependent", {
  host       = "h",
  command    = "mkdir -p %s && touch %s",
  depends_on = { "boom" },
})
`, h.remoteBase, marker))

			out, err := h.apply(cfg, false)
			if err == nil {
				t.Fatalf("expected apply to report failure, got nil error\n%s", out)
			}
			if !strings.Contains(out, "skipped") {
				t.Fatalf("expected 'dependent' to be reported as skipped, got:\n%s", out)
			}
			if _, code := h.exec("test -e " + marker); code == 0 {
				t.Fatalf("marker file should not exist: dependent should have been skipped")
			}
		}).
		Run()
}
