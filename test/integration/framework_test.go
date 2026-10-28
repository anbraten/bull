//go:build integration

package integration

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

const composeFile = "../docker-compose.yml"

var allTargets = []target{debianTarget, alpineTarget}

// TestMain is the Go equivalent of `./test/setup.sh up`, scoped to exactly
// what this test binary needs: a keypair, running containers, and that key
// provisioned into each of them. It runs once before any test in this
// package, so `go test -tags=integration ./test/integration/...` works on
// its own without a separate setup step.
func TestMain(m *testing.M) {
	if err := ensureEnvironment(); err != nil {
		fmt.Fprintln(os.Stderr, "integration setup failed:", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func ensureEnvironment() error {
	keyPath, err := ensureKeypair()
	if err != nil {
		return fmt.Errorf("ssh keypair: %w", err)
	}

	if !allReachable(allTargets) {
		if err := dockerComposeUp(); err != nil {
			return fmt.Errorf("start containers: %w", err)
		}
	}

	for _, tgt := range allTargets {
		if err := waitReachable(tgt.addr, 30*time.Second); err != nil {
			return fmt.Errorf("%s never became reachable: %w", tgt.name, err)
		}
		if err := provisionKey(tgt.service, keyPath+".pub"); err != nil {
			return fmt.Errorf("provision key on %s: %w", tgt.service, err)
		}
	}
	return nil
}

func allReachable(targets []target) bool {
	for _, tgt := range targets {
		conn, err := net.DialTimeout("tcp", tgt.addr, time.Second)
		if err != nil {
			return false
		}
		conn.Close()
	}
	return true
}

func waitReachable(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("not reachable after %s: %w", timeout, lastErr)
}

func dockerComposeUp() error {
	cmd := exec.Command("docker", "compose", "-f", composeFile, "up", "-d", "--build")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ensureKeypair generates the shared test keypair with the standard library
// (no ssh-keygen binary needed) if it doesn't already exist, and returns the
// private key path.
func ensureKeypair() (string, error) {
	dir, err := filepath.Abs("../config/.ssh")
	if err != nil {
		return "", err
	}
	priv := filepath.Join(dir, "id_ed25519")
	pub := priv + ".pub"

	if _, err := os.Stat(priv); err == nil {
		if _, err := os.Stat(pub); err == nil {
			return priv, nil
		}
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}

	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}

	block, err := ssh.MarshalPrivateKey(privKey, "bull-test-key")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(priv, pem.EncodeToMemory(block), 0o600); err != nil {
		return "", err
	}

	sshPub, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(pub, ssh.MarshalAuthorizedKey(sshPub), 0o644); err != nil {
		return "", err
	}
	return priv, nil
}

// provisionKey appends pubKeyPath's contents to the container's
// authorized_keys, piping it in over stdin so no shell-quoting is needed.
func provisionKey(service, pubKeyPath string) error {
	data, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return err
	}

	const script = `set -e
mkdir -p /root/.ssh
chmod 700 /root/.ssh
cat > /tmp/bull_test_key.pub
touch /root/.ssh/authorized_keys
if ! grep -qxF "$(cat /tmp/bull_test_key.pub)" /root/.ssh/authorized_keys; then
  cat /tmp/bull_test_key.pub >> /root/.ssh/authorized_keys
fi
chmod 600 /root/.ssh/authorized_keys
rm -f /tmp/bull_test_key.pub
`

	cmd := exec.Command("docker", "compose", "-f", composeFile, "exec", "-T", service, "sh", "-c", script)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Suite groups integration cases that run against the same target and share
// one harness, so BeforeAll/AfterAll can arrange and check state across all
// of them. Build one with NewSuite, chain Case for each scenario, and finish
// with Run:
//
//	NewSuite(t, debianTarget).
//		BeforeAll(func(t *testing.T, h *harness) { ... }).
//		Case("does_the_thing", func(t *testing.T, h *harness) { ... }).
//		AfterAll(func(t *testing.T, h *harness) { ... }).
//		Run()
type Suite struct {
	t      *testing.T
	target target
	before func(t *testing.T, h *harness)
	after  func(t *testing.T, h *harness)
	cases  []suiteCase
}

type suiteCase struct {
	name string
	run  func(t *testing.T, h *harness)
}

// NewSuite starts a suite of cases run against tgt.
func NewSuite(t *testing.T, tgt target) *Suite {
	return &Suite{t: t, target: tgt}
}

// BeforeAll registers setup that runs once, before any case, against the
// harness every case in the suite shares.
func (s *Suite) BeforeAll(fn func(t *testing.T, h *harness)) *Suite {
	s.before = fn
	return s
}

// AfterAll registers teardown that runs once, after every case has run
// (even if one failed), against that same shared harness.
func (s *Suite) AfterAll(fn func(t *testing.T, h *harness)) *Suite {
	s.after = fn
	return s
}

// Case registers one scenario, run as its own subtest.
func (s *Suite) Case(name string, run func(t *testing.T, h *harness)) *Suite {
	s.cases = append(s.cases, suiteCase{name, run})
	return s
}

// Run creates the shared harness, runs BeforeAll, executes every registered
// case as its own subtest, then runs AfterAll.
func (s *Suite) Run() {
	s.t.Helper()
	h := newHarness(s.t, s.target)

	if s.after != nil {
		s.t.Cleanup(func() { s.after(s.t, h) })
	}
	if s.before != nil {
		s.before(s.t, h)
	}

	for _, c := range s.cases {
		c := c
		s.t.Run(c.name, func(t *testing.T) {
			c.run(t, h)
		})
	}
}
