//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type stateResource struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type stateFile struct {
	Resources []stateResource `json:"resources"`
}

func readState(t *testing.T, configFile string) stateFile {
	t.Helper()
	dir := filepath.Dir(configFile)
	base := strings.TrimSuffix(filepath.Base(configFile), filepath.Ext(configFile))
	path := filepath.Join(dir, base+".state.json")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state file %s: %v", path, err)
	}
	var s stateFile
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("parse state file %s: %v", path, err)
	}
	return s
}

func hasResource(s stateFile, id string) bool {
	for _, r := range s.Resources {
		if r.ID == id {
			return true
		}
	}
	return false
}

// TestOrphanDetectionAndPrune documents current behavior: removing a
// resource from config marks it orphaned in state, but bull never deletes
// anything from the actual infrastructure — --prune only forgets the
// resource in the local state file. See TODO.md: a real "bull destroy"
// still needs per-module delete support.
func TestOrphanDetectionAndPrune(t *testing.T) {
	NewSuite(t, debianTarget).
		BeforeAll(func(t *testing.T, h *harness) {
			// Arrange: apply a config with two resources, so "gone" exists
			// in both state and on the remote host before the case removes
			// it from config.
			cfg := h.writeConfig(fmt.Sprintf(`
file("keep", { host = "h", path = %q, content = "kept\n" })
file("gone", { host = "h", path = %q, content = "will be orphaned\n" })
`, h.remotePath("keep.txt"), h.remotePath("gone.txt")))

			if out, err := h.apply(cfg, false); err != nil {
				t.Fatalf("initial apply failed: %v\n%s", err, out)
			}
		}).
		Case("prune_forgets_state_but_leaves_remote_file", func(t *testing.T, h *harness) {
			keepPath := h.remotePath("keep.txt")
			gonePath := h.remotePath("gone.txt")

			st := readState(t, h.configPath())
			if !hasResource(st, "gone") {
				t.Fatalf("expected state to contain 'gone' resource after initial apply")
			}

			// Re-declare the config without "gone" -> it shows up as orphaned.
			onlyKeep := h.writeConfig(fmt.Sprintf(`
file("keep", { host = "h", path = %q, content = "kept\n" })
`, keepPath))

			out, err := h.apply(onlyKeep, false)
			if err != nil {
				t.Fatalf("apply without prune failed: %v\n%s", err, out)
			}
			if !strings.Contains(out, "orphaned") {
				t.Fatalf("expected an orphan warning, got:\n%s", out)
			}

			st = readState(t, onlyKeep)
			if !hasResource(st, "gone") {
				t.Fatalf("orphan should remain in state until --prune is used")
			}
			if _, code := h.exec("test -f " + gonePath); code != 0 {
				t.Fatalf("orphaned remote file should still exist (bull cannot destroy it yet)")
			}

			// Apply again with --prune -> forgotten in state, but still not
			// deleted from the actual host.
			if out, err := h.apply(onlyKeep, true); err != nil {
				t.Fatalf("apply with prune failed: %v\n%s", err, out)
			}

			st = readState(t, onlyKeep)
			if hasResource(st, "gone") {
				t.Fatalf("orphan should be removed from state after --prune")
			}
			if _, code := h.exec("test -f " + gonePath); code != 0 {
				t.Fatalf("orphaned remote file should still exist after --prune (state-only removal)")
			}
		}).
		AfterAll(func(t *testing.T, h *harness) {
			// Sanity check the suite didn't disturb the resource that was
			// never meant to be touched, regardless of how the case above went.
			if _, code := h.exec("test -f " + h.remotePath("keep.txt")); code != 0 {
				t.Errorf("expected 'keep' file to still exist once the suite finished")
			}
		}).
		Run()
}
