package ssh

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Host defines connection parameters for a remote host.
type Host struct {
	Name     string
	Addr     string // host:port, defaults to port 22
	User     string
	Password string
	KeyFile  string // path to private key, defaults to ~/.ssh/id_rsa
	Insecure bool   // skip host key verification (useful for test containers)
}

// Pool manages SSH connections, reusing them across calls.
type Pool struct {
	mu      sync.Mutex
	hosts   map[string]*Host
	clients map[string]*gossh.Client
	errors  map[string]error // cached connection failures
}

func NewPool() *Pool {
	return &Pool{
		hosts:   make(map[string]*Host),
		clients: make(map[string]*gossh.Client),
		errors:  make(map[string]error),
	}
}

func (p *Pool) RegisterHost(h *Host) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if h.Addr != "" && !strings.Contains(h.Addr, ":") {
		h.Addr = h.Addr + ":22"
	}
	p.hosts[h.Name] = h
}

func (p *Pool) client(hostName string) (*gossh.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if c, ok := p.clients[hostName]; ok {
		return c, nil
	}
	if err, ok := p.errors[hostName]; ok {
		return nil, err // don't retry a failed host
	}

	h, ok := p.hosts[hostName]
	if !ok {
		return nil, fmt.Errorf("unknown host %q (did you declare it with host()?)", hostName)
	}

	cfg, err := buildSSHConfig(h)
	if err != nil {
		p.errors[hostName] = err
		return nil, fmt.Errorf("ssh config for %q: %w", hostName, err)
	}

	addr := h.Addr
	if addr == "" {
		err = fmt.Errorf("host %q has no address", hostName)
		p.errors[hostName] = err
		return nil, err
	}

	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		p.errors[hostName] = err
		return nil, fmt.Errorf("connect to %s: %w", addr, err)
	}
	sshConn, chans, reqs, err := gossh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		p.errors[hostName] = err
		return nil, fmt.Errorf("ssh handshake with %s: %w", addr, err)
	}
	c := gossh.NewClient(sshConn, chans, reqs)

	p.clients[hostName] = c
	return c, nil
}

// Run executes a command on the named host and returns combined stdout+stderr.
func (p *Pool) Run(hostName, cmd string) (string, error) {
	client, err := p.client(hostName)
	if err != nil {
		return "", err
	}

	sess, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("new session: %w", err)
	}
	defer sess.Close()

	var buf bytes.Buffer
	sess.Stdout = &buf
	sess.Stderr = &buf

	if err := sess.Run(cmd); err != nil {
		return buf.String(), fmt.Errorf("command %q: %w\n%s", cmd, err, buf.String())
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// Exec runs a command and returns stdout, exit code, and any connection error.
// exit code is -1 when a connection/session error occurred (errMsg will be set).
// A non-zero exit code from the command itself is NOT an error — callers check it.
func (p *Pool) Exec(hostName, cmd string) (stdout string, exitCode int, errMsg string) {
	client, err := p.client(hostName)
	if err != nil {
		return "", -1, err.Error()
	}

	sess, err := client.NewSession()
	if err != nil {
		return "", -1, fmt.Sprintf("new session: %v", err)
	}
	defer sess.Close()

	var outBuf, errBuf bytes.Buffer
	sess.Stdout = &outBuf
	sess.Stderr = &errBuf

	runErr := sess.Run(cmd)
	out := strings.TrimRight(outBuf.String()+errBuf.String(), "\n")
	if runErr != nil {
		if exitErr, ok := runErr.(*gossh.ExitError); ok {
			return out, exitErr.ExitStatus(), ""
		}
		return out, -1, runErr.Error()
	}
	return out, 0, ""
}

// RunOutput runs a command and returns only stdout, stderr separately.
func (p *Pool) RunOutput(hostName, cmd string) (stdout, stderr string, err error) {
	client, err := p.client(hostName)
	if err != nil {
		return "", "", err
	}

	sess, err := client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("new session: %w", err)
	}
	defer sess.Close()

	var outBuf, errBuf bytes.Buffer
	sess.Stdout = &outBuf
	sess.Stderr = &errBuf

	runErr := sess.Run(cmd)
	return strings.TrimRight(outBuf.String(), "\n"),
		strings.TrimRight(errBuf.String(), "\n"),
		runErr
}

// Upload copies content to a remote path via stdin.
func (p *Pool) Upload(hostName, remotePath string, content []byte, mode os.FileMode) error {
	client, err := p.client(hostName)
	if err != nil {
		return err
	}

	// Ensure parent directory exists
	dir := filepath.Dir(remotePath)
	if dir != "." && dir != "/" {
		mkdirSess, err := client.NewSession()
		if err != nil {
			return err
		}
		_ = mkdirSess.Run(fmt.Sprintf("mkdir -p %q", dir))
		mkdirSess.Close()
	}

	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer sess.Close()

	sess.Stdin = bytes.NewReader(content)
	cmd := fmt.Sprintf("cat > %q && chmod %04o %q", remotePath, mode, remotePath)
	if err := sess.Run(cmd); err != nil {
		return fmt.Errorf("upload to %s: %w", remotePath, err)
	}
	return nil
}

// Download fetches a remote file's contents.
func (p *Pool) Download(hostName, remotePath string) ([]byte, error) {
	client, err := p.client(hostName)
	if err != nil {
		return nil, err
	}

	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()

	var buf bytes.Buffer
	sess.Stdout = &buf
	if err := sess.Run(fmt.Sprintf("cat %q", remotePath)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// FileExists checks if a remote path exists.
func (p *Pool) FileExists(hostName, remotePath string) (bool, error) {
	stdout, _, err := p.RunOutput(hostName, fmt.Sprintf("test -e %q && echo yes || echo no", remotePath))
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(stdout) == "yes", nil
}

// Close closes all open connections.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.clients {
		c.Close()
	}
}

func buildSSHConfig(h *Host) (*gossh.ClientConfig, error) {
	var authMethods []gossh.AuthMethod

	if h.Password != "" {
		authMethods = append(authMethods, gossh.Password(h.Password))
	}

	keyFile := h.KeyFile
	if keyFile == "" {
		// Try common default key locations
		for _, name := range []string{"id_ed25519", "id_rsa", "id_ecdsa"} {
			path := filepath.Join(os.Getenv("HOME"), ".ssh", name)
			if _, err := os.Stat(path); err == nil {
				keyFile = path
				break
			}
		}
	}

	if keyFile != "" {
		signer, err := loadKey(expandHome(keyFile))
		if err != nil {
			return nil, fmt.Errorf("load key %s: %w", keyFile, err)
		}
		authMethods = append(authMethods, gossh.PublicKeys(signer))
	}

	if len(authMethods) == 0 {
		return nil, fmt.Errorf("no authentication method for host %q", h.Name)
	}

	insecureCallback := gossh.HostKeyCallback(func(hostname string, remote net.Addr, key gossh.PublicKey) error {
		return nil
	})
	var hostKeyCallback gossh.HostKeyCallback
	if h.Insecure {
		hostKeyCallback = insecureCallback
	} else {
		var err error
		hostKeyCallback, err = buildHostKeyCallback()
		if err != nil {
			// Fall back to insecure if known_hosts not available
			hostKeyCallback = insecureCallback
		}
	}

	user := h.User
	if user == "" {
		user = os.Getenv("USER")
	}

	return &gossh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
	}, nil
}

func loadKey(path string) (gossh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := gossh.ParsePrivateKey(data)
	if err != nil {
		return nil, err
	}
	return signer, nil
}

func buildHostKeyCallback() (gossh.HostKeyCallback, error) {
	knownHostsFile := filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts")
	if _, err := os.Stat(knownHostsFile); err != nil {
		return nil, err
	}
	return knownhosts.New(knownHostsFile)
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(os.Getenv("HOME"), path[2:])
	}
	return path
}

