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
}

func (r *Registry) Pool() *sshpool.Pool { return r.pool }

func (r *Registry) Close() { r.L.Close() }

// Eval executes a Lua config file and returns the registry.
func Eval(filePath string) (*Registry, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, err
	}

	pool := sshpool.NewPool()
	reg := &Registry{pool: pool}

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

	registerPrimitives(L, reg, pool)

	if err := loadStdlib(L); err != nil {
		L.Close()
		return nil, fmt.Errorf("load stdlib: %w", err)
	}

	// Auto-load all *.lua files from bull_modules/ (alphabetical order).
	// This makes provider modules available as globals without explicit require.
	if err := loadModules(L, modulesDir); err != nil {
		L.Close()
		return nil, fmt.Errorf("bull_modules: %w", err)
	}

	if err := L.DoFile(absPath); err != nil {
		L.Close()
		return nil, fmt.Errorf("lua: %w", err)
	}

	return reg, nil
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
func loadModules(L *lua.LState, dir string) error {
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
	}
	return nil
}

func registerPrimitives(L *lua.LState, reg *Registry, pool *sshpool.Pool) {

	// host("name", { addr=, user=, key=, password= })
	L.SetGlobal("host", L.NewFunction(func(L *lua.LState) int {
		name := L.CheckString(1)
		opts := L.CheckTable(2)
		insecure := false
		if v := opts.RawGetString("insecure"); v != lua.LNil {
			insecure = bool(lua.LVAsBool(v))
		}
		h := &sshpool.Host{
			Name:     name,
			Addr:     tStr(opts, "addr"),
			User:     tStr(opts, "user"),
			Password: tStr(opts, "password"),
			KeyFile:  tStrAlt(opts, "key", "key_file"),
			Insecure: insecure,
		}
		reg.Hosts = append(reg.Hosts, h)
		pool.RegisterHost(h)
		return 0
	}))

	// resource("id", { type=, plan=fn, apply=fn })
	L.SetGlobal("resource", L.NewFunction(func(L *lua.LState) int {
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

	// ssh_exec(host, cmd) → stdout, exit_code, err_msg
	// exit_code=-1 and err_msg!="" means SSH/connection failure
	L.SetGlobal("ssh_exec", L.NewFunction(func(L *lua.LState) int {
		hostName := L.CheckString(1)
		cmd := L.CheckString(2)
		stdout, code, errMsg := pool.Exec(hostName, cmd)
		L.Push(lua.LString(stdout))
		L.Push(lua.LNumber(code))
		L.Push(lua.LString(errMsg))
		return 3
	}))

	// ssh_upload(host, remote_path, content [, mode]) → err_msg
	L.SetGlobal("ssh_upload", L.NewFunction(func(L *lua.LState) int {
		hostName := L.CheckString(1)
		path := L.CheckString(2)
		content := L.CheckString(3)
		mode := os.FileMode(L.OptInt(4, 0o644))
		if err := pool.Upload(hostName, path, []byte(content), mode); err != nil {
			L.Push(lua.LString(err.Error()))
		} else {
			L.Push(lua.LString(""))
		}
		return 1
	}))

	// local_exec(cmd) → stdout, exit_code, err_msg
	L.SetGlobal("local_exec", L.NewFunction(func(L *lua.LState) int {
		cmdStr := L.CheckString(1)
		stdout, code, errMsg := runLocal(cmdStr)
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

	// read_file(path) → content  (errors if not found)
	L.SetGlobal("read_file", L.NewFunction(func(L *lua.LState) int {
		path := L.CheckString(1)
		data, err := os.ReadFile(path)
		if err != nil {
			L.RaiseError("read_file: %v", err)
		}
		L.Push(lua.LString(data))
		return 1
	}))

	// sha256(content) → full hex string
	L.SetGlobal("sha256", L.NewFunction(func(L *lua.LState) int {
		content := L.CheckString(1)
		h := sha256.Sum256([]byte(content))
		L.Push(lua.LString(fmt.Sprintf("%x", h)))
		return 1
	}))

	// json_decode(str) → table, err_msg
	L.SetGlobal("json_decode", L.NewFunction(func(L *lua.LState) int {
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

	// json_encode(table) → str, err_msg
	L.SetGlobal("json_encode", L.NewFunction(func(L *lua.LState) int {
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

	// q(str) → single-quoted shell-safe string
	L.SetGlobal("q", L.NewFunction(func(L *lua.LState) int {
		s := L.CheckString(1)
		escaped := strings.ReplaceAll(s, "'", `'\''`)
		L.Push(lua.LString("'" + escaped + "'"))
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
