//go:build integration

// Package integration drives real bull plan/apply runs against disposable
// SSH containers. It's excluded from normal `go test ./...` runs (build tag
// "integration") since it needs Docker.
//
// TestMain (framework_test.go) brings up the containers and provisions the
// test key on its own, so just run:
//
//	go test -tags=integration ./test/integration/...
package integration

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anbraten/bull/internal/engine"
	sshpool "github.com/anbraten/bull/internal/ssh"
)

// target describes one of the containers started by test/docker-compose.yml.
type target struct {
	name    string // human-readable, used in test/skip messages
	service string // docker-compose service name
	addr    string
}

var (
	debianTarget = target{name: "debian", service: "sshd", addr: "localhost:2223"}
	alpineTarget = target{name: "alpine", service: "sshd-alpine", addr: "localhost:2224"}
)

// harness runs bull against a single target inside an isolated remote
// directory, so tests can't interfere with each other even when reusing the
// same long-lived container.
type harness struct {
	t          *testing.T
	dir        string // temp dir holding generated configs + copied bull_modules
	target     target
	remoteBase string
	keyPath    string
	pool       *sshpool.Pool
}

func newHarness(t *testing.T, tgt target) *harness {
	t.Helper()

	keyPath, err := filepath.Abs("../config/.ssh/id_ed25519")
	if err != nil {
		t.Fatalf("resolve key path: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("test key not found at %s (run ./test/setup.sh up first): %v", keyPath, err)
	}

	dir := t.TempDir()
	copyModules(t, dir)

	pool := sshpool.NewPool()
	pool.RegisterHost(&sshpool.Host{
		Name:     "h",
		Addr:     tgt.addr,
		User:     "root",
		KeyFile:  keyPath,
		Insecure: true,
	})

	h := &harness{
		t:          t,
		dir:        dir,
		target:     tgt,
		remoteBase: "/opt/bulltest-" + sanitize(t.Name()),
		keyPath:    keyPath,
		pool:       pool,
	}

	// Start from a clean remote directory so assertions are deterministic
	// across repeated local test runs against the same long-lived container.
	h.pool.Run("h", "rm -rf "+h.remoteBase)

	t.Cleanup(func() {
		h.pool.Run("h", "rm -rf "+h.remoteBase)
		h.pool.Close()
	})

	return h
}

func copyModules(t *testing.T, dstDir string) {
	t.Helper()
	srcDir := "../config/bull_modules"
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("read bull_modules: %v", err)
	}
	dst := filepath.Join(dstDir, "bull_modules")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("mkdir bull_modules: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".lua") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			t.Fatalf("read module %s: %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644); err != nil {
			t.Fatalf("write module %s: %v", e.Name(), err)
		}
	}
}

func sanitize(s string) string {
	repl := strings.NewReplacer("/", "-", " ", "-")
	return repl.Replace(s)
}

// remotePath builds a path under this test's isolated remote directory.
func (h *harness) remotePath(name string) string {
	return h.remoteBase + "/" + name
}

// configPath returns the fixed config path writeConfig always writes to.
func (h *harness) configPath() string {
	return filepath.Join(h.dir, "infra.lua")
}

// writeConfig writes body (plus a "h" host() header pointing at this
// harness's target) to a fixed infra.lua in the temp dir, so repeated calls
// within a test share the same config path -> the same state file.
func (h *harness) writeConfig(body string) string {
	h.t.Helper()
	header := fmt.Sprintf(`host("h", {
  addr = %q,
  user = "root",
  key = %q,
  insecure = true,
})

`, h.target.addr, h.keyPath)

	path := filepath.Join(h.dir, "infra.lua")
	if err := os.WriteFile(path, []byte(header+body), 0o644); err != nil {
		h.t.Fatalf("write config: %v", err)
	}
	return path
}

// apply runs `bull apply` against file, capturing everything it prints.
func (h *harness) apply(file string, prune bool) (stdout string, err error) {
	h.t.Helper()
	return captureStdout(h.t, func() error {
		return engine.New(false, nil).Apply(file, true, prune)
	})
}

// plan runs `bull plan` against file, capturing everything it prints.
func (h *harness) plan(file string) (stdout string, err error) {
	h.t.Helper()
	return captureStdout(h.t, func() error {
		return engine.New(false, nil).Plan(file)
	})
}

// exec runs cmd on the target directly (bypassing bull) to verify effects.
// It fails the test on connection errors; the command's own exit code is
// returned to the caller to assert on.
func (h *harness) exec(cmd string) (stdout string, exitCode int) {
	h.t.Helper()
	out, code, errMsg := h.pool.Exec("h", cmd)
	if errMsg != "" {
		h.t.Fatalf("ssh exec %q: %s", cmd, errMsg)
	}
	return out, code
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	os.Stdout = w

	fnErr := fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return buf.String(), fnErr
}
