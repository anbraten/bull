package lua

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	sshpool "github.com/anbraten/bull/internal/ssh"
	lua "github.com/yuin/gopher-lua"
)

//go:embed stdlib/*.lua
var stdlibFS embed.FS

// stdlibOrder lists the core embedded modules loaded before any user config.
var stdlibOrder = []string{
	"stdlib/utils.lua",
	"stdlib/shell.lua",
	"stdlib/file.lua",
	"stdlib/docker_compose.lua",
}

// Resource is a plan+apply pair registered from Lua via resource().
type Resource struct {
	ID        string
	Type      string
	DependsOn []string
	PlanFn    *lua.LFunction
	ApplyFn   *lua.LFunction
}

// Registry holds everything collected from a Lua config evaluation.
type Registry struct {
	Hosts     []*sshpool.Host
	Resources []*Resource
	pool      *sshpool.Pool
	L         *lua.LState // kept alive so plan/apply functions can be called
	// SecretValues holds every value returned by secret(), including
	// defaults, keyed by secret name. Output must redact these.
	SecretValues map[string]string
	// Warnings collects non-fatal issues found while loading, such as a
	// module redefining a built-in resource type.
	Warnings []string
}

func (r *Registry) Pool() *sshpool.Pool { return r.pool }

func (r *Registry) Close() { r.L.Close() }

// Eval executes a Lua config file and returns the registry.
func Eval(filePath string, verbose bool, secrets map[string]string) (*Registry, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, err
	}

	pool := sshpool.NewPool()
	reg := &Registry{pool: pool, SecretValues: map[string]string{}}

	L := lua.NewState()
	reg.L = L

	// Make require() resolve relative to the config file's directory,
	// and also from bull_modules/ so require("hetzner_dns") just works.
	dir := filepath.Dir(absPath)
	modulesDir := filepath.Join(dir, "bull_modules")
	L.SetField(L.GetGlobal("package"), "path", lua.LString(strings.Join([]string{
		dir + "/?.lua",
		dir + "/?/init.lua",
		modulesDir + "/?.lua",
		modulesDir + "/?/init.lua",
	}, ";")))

	registerPrimitives(L, reg, pool, verbose, secrets, dir)

	if err := loadStdlib(L); err != nil {
		L.Close()
		return nil, fmt.Errorf("load stdlib: %w", err)
	}

	globals := newGlobalTracker(L, "built-in")

	// Auto-load all *.lua files from bull_modules/ (alphabetical order).
	// This makes provider modules available as globals without explicit require.
	if err := loadModules(L, modulesDir, globals); err != nil {
		L.Close()
		return nil, fmt.Errorf("bull_modules: %w", err)
	}

	if err := L.DoFile(absPath); err != nil {
		L.Close()
		return nil, fmt.Errorf("lua: %w", err)
	}
	globals.check(filepath.Base(absPath))

	reg.Warnings = globals.warnings
	for _, w := range reg.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}

	return reg, nil
}

// globalTracker records which file defined each global, so that a module or
// config silently replacing an existing global (e.g. two modules both
// defining apt, or a module redefining file) becomes a visible warning.
type globalTracker struct {
	L        *lua.LState
	values   map[string]lua.LValue
	sources  map[string]string
	warnings []string
}

func newGlobalTracker(L *lua.LState, source string) *globalTracker {
	g := &globalTracker{L: L, values: map[string]lua.LValue{}, sources: map[string]string{}}
	L.G.Global.ForEach(func(k, v lua.LValue) {
		if name, ok := k.(lua.LString); ok {
			g.values[string(name)] = v
			g.sources[string(name)] = source
		}
	})
	return g
}

// check records globals set by source and warns about any it replaced.
func (g *globalTracker) check(source string) {
	var overridden []string
	g.L.G.Global.ForEach(func(k, v lua.LValue) {
		name, ok := k.(lua.LString)
		if !ok {
			return
		}
		prev, seen := g.values[string(name)]
		if seen && prev == v {
			return
		}
		if seen {
			overridden = append(overridden, fmt.Sprintf("%s overrides %q from %s", source, string(name), g.sources[string(name)]))
		}
		g.values[string(name)] = v
		g.sources[string(name)] = source
	})
	sort.Strings(overridden) // ForEach order is not deterministic
	g.warnings = append(g.warnings, overridden...)
}

func loadStdlib(L *lua.LState) error {
	for _, path := range stdlibOrder {
		data, err := stdlibFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("missing stdlib file %s: %w", path, err)
		}
		if err := L.DoString(string(data)); err != nil {
			return fmt.Errorf("stdlib %s: %w", path, err)
		}
	}
	return nil
}

// loadModules auto-loads all *.lua files from a bull_modules/ directory.
// Missing directory is silently ignored; file errors are returned.
func loadModules(L *lua.LState, dir string, globals *globalTracker) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".lua") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files) // deterministic load order

	for _, path := range files {
		if err := L.DoFile(path); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		globals.check("bull_modules/" + filepath.Base(path))
	}
	return nil
}

func registerPrimitives(L *lua.LState, reg *Registry, pool *sshpool.Pool, verbose bool, secrets map[string]string, configDir string) {
	if secrets == nil {
		secrets = map[string]string{}
	}

	verbosef := func(format string, args ...any) {
		if !verbose {
			return
		}
		fmt.Printf("[verbose] "+format+"\n", args...)
	}

	// Primitives for module authors live under the "bull" table so they never
	// collide with user-defined globals. host/env/secret stay global as part of
	// the config DSL.
	bull := L.NewTable()
	sshTbl := L.NewTable()
	jsonTbl := L.NewTable()
	L.SetField(bull, "ssh", sshTbl)
	L.SetField(bull, "json", jsonTbl)
	L.SetGlobal("bull", bull)

	// host("name", { addr=, user=, key=, password= })
	L.SetGlobal("host", L.NewFunction(func(L *lua.LState) int {
		name := L.CheckString(1)
		opts := L.CheckTable(2)
		insecure := false
		if v := opts.RawGetString("insecure"); v != lua.LNil {
			insecure = bool(lua.LVAsBool(v))
		}
		keyPath := tStrAlt(opts, "key", "key_file")
		h := &sshpool.Host{
			Name:     name,
			Addr:     tStr(opts, "addr"),
			User:     tStr(opts, "user"),
			Password: tStr(opts, "password"),
			KeyFile:  resolveLocalPath(keyPath, configDir),
			Insecure: insecure,
		}
		reg.Hosts = append(reg.Hosts, h)
		pool.RegisterHost(h)
		return 0
	}))

	// bull.resource("id", { type=, plan=fn, apply=fn })
	L.SetField(bull, "resource", L.NewFunction(func(L *lua.LState) int {
		id := L.CheckString(1)
		opts := L.CheckTable(2)

		for _, r := range reg.Resources {
			if r.ID == id {
				L.RaiseError("duplicate resource id %q", id)
			}
		}

		planFn, ok := opts.RawGetString("plan").(*lua.LFunction)
		if !ok {
			L.RaiseError("resource %q: plan must be a function", id)
		}
		applyFn, ok := opts.RawGetString("apply").(*lua.LFunction)
		if !ok {
			L.RaiseError("resource %q: apply must be a function", id)
		}

		var deps []string
		if depsVal := opts.RawGetString("depends_on"); depsVal != lua.LNil {
			if depsTbl, ok := depsVal.(*lua.LTable); ok {
				depsTbl.ForEach(func(_, v lua.LValue) {
					if s, ok := v.(lua.LString); ok {
						deps = append(deps, string(s))
					}
				})
			}
		}

		reg.Resources = append(reg.Resources, &Resource{
			ID:        id,
			Type:      tStr(opts, "type"),
			DependsOn: deps,
			PlanFn:    planFn,
			ApplyFn:   applyFn,
		})
		return 0
	}))

	// bull.ssh.exec(host, cmd) → stdout, exit_code, err_msg
	// exit_code=-1 and err_msg!="" means SSH/connection failure
	L.SetField(sshTbl, "exec", L.NewFunction(func(L *lua.LState) int {
		hostName := L.CheckString(1)
		cmd := L.CheckString(2)
		verbosef("bull.ssh.exec(host=%q, cmd=%q)", hostName, cmd)
		stdout, code, errMsg := pool.Exec(hostName, cmd)
		verbosef("bull.ssh.exec -> stdout=%q exit_code=%d err_msg=%q", stdout, code, errMsg)
		L.Push(lua.LString(stdout))
		L.Push(lua.LNumber(code))
		L.Push(lua.LString(errMsg))
		return 3
	}))

	// bull.ssh.upload(host, remote_path, content [, mode]) → err_msg
	L.SetField(sshTbl, "upload", L.NewFunction(func(L *lua.LState) int {
		hostName := L.CheckString(1)
		path := L.CheckString(2)
		content := L.CheckString(3)
		mode := os.FileMode(L.OptInt(4, 0o644))
		verbosef("bull.ssh.upload(host=%q, path=%q, bytes=%d, mode=%#o)", hostName, path, len(content), mode)
		if err := pool.Upload(hostName, path, []byte(content), mode); err != nil {
			verbosef("bull.ssh.upload -> err_msg=%q", err.Error())
			L.Push(lua.LString(err.Error()))
		} else {
			verbosef("bull.ssh.upload -> err_msg=%q", "")
			L.Push(lua.LString(""))
		}
		return 1
	}))

	// bull.ssh.read(host, remote_path) → content, err_msg
	// Returns the file's raw bytes (unlike bull.ssh.exec, nothing is trimmed).
	L.SetField(sshTbl, "read", L.NewFunction(func(L *lua.LState) int {
		hostName := L.CheckString(1)
		path := L.CheckString(2)
		verbosef("bull.ssh.read(host=%q, path=%q)", hostName, path)
		data, err := pool.Download(hostName, path)
		if err != nil {
			verbosef("bull.ssh.read -> err_msg=%q", err.Error())
			L.Push(lua.LString(""))
			L.Push(lua.LString(err.Error()))
			return 2
		}
		verbosef("bull.ssh.read -> bytes=%d", len(data))
		L.Push(lua.LString(data))
		L.Push(lua.LString(""))
		return 2
	}))

	// bull.exec(cmd) → stdout, exit_code, err_msg
	L.SetField(bull, "exec", L.NewFunction(func(L *lua.LState) int {
		cmdStr := L.CheckString(1)
		verbosef("bull.exec(cmd=%q)", cmdStr)
		stdout, code, errMsg := runLocal(cmdStr)
		verbosef("bull.exec -> stdout=%q exit_code=%d err_msg=%q", stdout, code, errMsg)
		L.Push(lua.LString(stdout))
		L.Push(lua.LNumber(code))
		L.Push(lua.LString(errMsg))
		return 3
	}))

	// env(name [, default]) → value  (errors if not set and no default given)
	L.SetGlobal("env", L.NewFunction(func(L *lua.LState) int {
		name := L.CheckString(1)
		val := os.Getenv(name)
		if val == "" {
			if L.GetTop() >= 2 {
				L.Push(lua.LString(L.OptString(2, "")))
			} else {
				L.RaiseError("environment variable %q is not set", name)
			}
		} else {
			L.Push(lua.LString(val))
		}
		return 1
	}))

	// secret(name [, default]) → value  (errors if not set and no default given)
	L.SetGlobal("secret", L.NewFunction(func(L *lua.LState) int {
		name := L.CheckString(1)
		if val, ok := secrets[name]; ok {
			reg.SecretValues[name] = val
			L.Push(lua.LString(val))
			return 1
		}
		if L.GetTop() >= 2 {
			val := L.OptString(2, "")
			reg.SecretValues[name] = val
			L.Push(lua.LString(val))
			return 1
		}
		L.RaiseError("secret %q is not set", name)
		return 0
	}))

	// bull.read_file(path) → content  (errors if not found)
	L.SetField(bull, "read_file", L.NewFunction(func(L *lua.LState) int {
		path := resolveLocalPath(L.CheckString(1), configDir)
		data, err := os.ReadFile(path)
		if err != nil {
			L.RaiseError("bull.read_file: %v", err)
		}
		L.Push(lua.LString(data))
		return 1
	}))

	// bull.sha256(content) → full hex string
	L.SetField(bull, "sha256", L.NewFunction(func(L *lua.LState) int {
		content := L.CheckString(1)
		h := sha256.Sum256([]byte(content))
		L.Push(lua.LString(fmt.Sprintf("%x", h)))
		return 1
	}))

	// bull.json.decode(str) → table, err_msg
	L.SetField(jsonTbl, "decode", L.NewFunction(func(L *lua.LState) int {
		s := L.CheckString(1)
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(goToLua(L, v))
		L.Push(lua.LNil)
		return 2
	}))

	// bull.json.encode(table) → str, err_msg
	L.SetField(jsonTbl, "encode", L.NewFunction(func(L *lua.LState) int {
		v := luaToGo(L.CheckAny(1))
		data, err := json.Marshal(v)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LString(data))
		L.Push(lua.LNil)
		return 2
	}))

	// bull.q(str) → single-quoted shell-safe string
	L.SetField(bull, "q", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LString(sshpool.ShellQuote(L.CheckString(1))))
		return 1
	}))
}

// runLocal runs a shell command and returns stdout, exit_code, err_msg.
func runLocal(cmdStr string) (stdout string, exitCode int, errMsg string) {
	cmd := exec.Command("sh", "-c", cmdStr)
	out, err := cmd.Output()
	stdout = strings.TrimRight(string(out), "\n")
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return stdout, exitErr.ExitCode(), ""
		}
		return "", -1, err.Error()
	}
	return stdout, 0, ""
}

// resolveLocalPath resolves relative local paths against the config directory.
func resolveLocalPath(path, configDir string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~/") || filepath.IsAbs(path) {
		return path
	}
	if configDir == "" {
		return path
	}
	return filepath.Clean(filepath.Join(configDir, path))
}
