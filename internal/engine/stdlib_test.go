package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	luaruntime "github.com/anbraten/bull/internal/lua"
)

// fakeRemote replaces the SSH primitives with local equivalents, so the real
// stdlib commands run against the local filesystem. Commands matching
// "docker compose" are recorded instead of run.
const fakeRemote = `
host("h", { addr = "unused:22", password = "unused" })
docker_calls = {}
function ssh_exec(h, cmd)
  if cmd:find("docker compose", 1, true) then
    table.insert(docker_calls, cmd)
    return "", 0, ""
  end
  return local_exec(cmd .. " 2>&1")
end
function ssh_upload(h, path, content, mode)
  local dir = path:match("^(.*)/[^/]*$")
  local _, code = local_exec("mkdir -p " .. q(dir))
  if code ~= 0 then return "mkdir failed" end
  local f, err = io.open(path, "wb")
  if not f then return err end
  f:write(content)
  f:close()
  local _, code = local_exec("chmod " .. string.format("%o", mode or 420) .. " " .. q(path))
  if code ~= 0 then return "chmod failed" end
  return ""
end
`

func stdlibConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "infra.lua")
	if err := os.WriteFile(path, []byte(fakeRemote+body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func planConfig(t *testing.T, cfg string) map[string]ResourcePlan {
	t.Helper()
	reg, err := luaruntime.Eval(cfg, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	out := map[string]ResourcePlan{}
	for _, p := range planAll(reg) {
		out[p.ID] = p
	}
	return out
}

func applyConfig(t *testing.T, cfg string) {
	t.Helper()
	if err := New(false, nil).Apply(cfg, true, false); err != nil {
		t.Fatal(err)
	}
}

func diffFields(p ResourcePlan) []string {
	var f []string
	for _, d := range p.Diffs {
		f = append(f, d.Field)
	}
	return f
}

func expectChange(t *testing.T, p ResourcePlan, want ChangeType, fields ...string) {
	t.Helper()
	if p.Err != nil {
		t.Fatalf("%s: unexpected error: %v", p.ID, p.Err)
	}
	if p.Change != want {
		t.Fatalf("%s: change = %s, want %s (diffs %+v)", p.ID, p.Change, want, p.Diffs)
	}
	if fields != nil && !reflect.DeepEqual(diffFields(p), fields) {
		t.Errorf("%s: diff fields = %v, want %v", p.ID, diffFields(p), fields)
	}
}

// luaStr quotes s as a Lua long string.
func luaStr(s string) string { return "[==[" + s + "]==]" }

func TestFileLifecycleLocal(t *testing.T) {
	// Path with characters that break naive quoting.
	target := filepath.Join(t.TempDir(), "we$ird dir", "it's.conf")
	write := func(content, mode string) string {
		return stdlibConfig(t, `file("f", { host = "h", path = `+luaStr(target)+`, content = `+luaStr(content)+`, mode = "`+mode+`" })`)
	}

	cfg := write("hello\n", "0644")
	expectChange(t, planConfig(t, cfg)["f"], ChangeCreate, "path", "content_hash", "mode")
	applyConfig(t, cfg)
	if data, _ := os.ReadFile(target); string(data) != "hello\n" {
		t.Fatalf("content = %q", data)
	}
	expectChange(t, planConfig(t, cfg)["f"], ChangeNoOp)

	cfg = write("changed\n", "0644")
	expectChange(t, planConfig(t, cfg)["f"], ChangeUpdate, "content_hash")

	cfg = write("hello\n", "0600")
	p := planConfig(t, cfg)["f"]
	expectChange(t, p, ChangeUpdate, "mode")
	if p.Diffs[0].Before != "644" || p.Diffs[0].After != "600" {
		t.Errorf("mode diff = %+v", p.Diffs[0])
	}
	applyConfig(t, cfg)
	if st, _ := os.Stat(target); st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o", st.Mode().Perm())
	}
	expectChange(t, planConfig(t, cfg)["f"], ChangeNoOp)
}

func TestFileUnreadableIsError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read anything")
	}
	target := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(target, []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	cfg := stdlibConfig(t, `file("f", { host = "h", path = `+luaStr(target)+`, content = "x" })`)
	p := planConfig(t, cfg)["f"]
	if p.Change != ChangeError {
		t.Fatalf("change = %s, want error", p.Change)
	}
}

func TestShellGuards(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	cfg := stdlibConfig(t, `
shell("creates", { host = "h", command = "touch "..q(`+luaStr(marker)+`), creates = `+luaStr(marker)+` })
shell("unless-true", { host = "h", command = "false", unless = "true" })
shell("unless-false", { host = "h", command = "true", unless = "false" })
shell("always", { host = "h", command = "true" })
`)
	plans := planConfig(t, cfg)
	expectChange(t, plans["creates"], ChangeCreate, "creates")
	expectChange(t, plans["unless-true"], ChangeNoOp)
	expectChange(t, plans["unless-false"], ChangeCreate, "command")
	expectChange(t, plans["always"], ChangeCreate, "command")

	applyConfig(t, cfg)
	plans = planConfig(t, cfg)
	expectChange(t, plans["creates"], ChangeNoOp)
	expectChange(t, plans["always"], ChangeCreate)
}

func TestShellFailureReportsOutput(t *testing.T) {
	cfg := stdlibConfig(t, `shell("s", { host = "h", command = "echo nope; exit 3" })`)
	err := New(false, nil).Apply(cfg, true, false)
	if err == nil || !strings.Contains(err.Error(), "command exited 3: nope") {
		t.Fatalf("got %v", err)
	}
}

func TestDockerComposeDetectsEnvChanges(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stack")
	write := func(compose, env string) string {
		envLine := ""
		if env != "" {
			envLine = ", env = " + env
		}
		return stdlibConfig(t, `docker_compose("app", { host = "h", dir = `+luaStr(dir)+`, compose = `+luaStr(compose)+envLine+` })`)
	}

	cfg := write("services: {}\n", `{ VERSION = "1", DEBUG = true }`)
	expectChange(t, planConfig(t, cfg)["app"], ChangeCreate, "host", "dir", "compose_hash", "env_hash")
	applyConfig(t, cfg)
	env, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if string(env) != "DEBUG=true\nVERSION=1\n" {
		t.Errorf(".env = %q", env)
	}
	if st, _ := os.Stat(filepath.Join(dir, ".env")); st.Mode().Perm() != 0o600 {
		t.Errorf(".env mode = %o", st.Mode().Perm())
	}
	expectChange(t, planConfig(t, cfg)["app"], ChangeNoOp)

	// Env-only change must trigger an update.
	cfg = write("services: {}\n", `{ VERSION = "2", DEBUG = true }`)
	expectChange(t, planConfig(t, cfg)["app"], ChangeUpdate, "env_hash")

	cfg = write("services: { a: {} }\n", `{ VERSION = "1", DEBUG = true }`)
	expectChange(t, planConfig(t, cfg)["app"], ChangeUpdate, "compose_hash")

	// Unmanaged .env is ignored.
	cfg = write("services: {}\n", "")
	expectChange(t, planConfig(t, cfg)["app"], ChangeNoOp)

	// Deleted .env is detected.
	cfg = write("services: {}\n", `{ VERSION = "1", DEBUG = true }`)
	os.Remove(filepath.Join(dir, ".env"))
	expectChange(t, planConfig(t, cfg)["app"], ChangeUpdate, "env_hash")
}
