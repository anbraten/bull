package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	luaruntime "github.com/anbraten/bull/internal/lua"
	lua "github.com/yuin/gopher-lua"
)

// writeConfig writes a Lua config into a temp dir and returns its path.
// Resources record applies by appending to a local "applied.log" file via
// local_exec, so tests can assert what actually ran without any SSH.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "infra.lua")
	prelude := `
local log = [[` + filepath.Join(dir, "applied.log") + `]]
function mark(id) local_exec("echo " .. q(id) .. " >> " .. q(log)) end
function ok(id, change, deps)
  resource(id, {
    type = "test",
    depends_on = deps,
    plan = function() return { change = change or "create" } end,
    apply = function() mark(id) end,
  })
end
function failing(id, deps)
  resource(id, {
    type = "test",
    depends_on = deps,
    plan = function() return { change = "create" } end,
    apply = function() error("boom") end,
  })
end
`
	if err := os.WriteFile(path, []byte(prelude+body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func appliedIDs(t *testing.T, cfg string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(cfg), "applied.log"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(data))
}

func stateIDs(t *testing.T, cfg string) []string {
	t.Helper()
	st, err := loadState(stateFilePath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range st.Resources {
		ids = append(ids, r.ID)
	}
	sort.Strings(ids)
	return ids
}

func setState(t *testing.T, cfg string, ids ...string) {
	t.Helper()
	var st State
	for _, id := range ids {
		st.Resources = append(st.Resources, StateResource{ID: id, Type: "test"})
	}
	if err := writeState(stateFilePath(cfg), st); err != nil {
		t.Fatal(err)
	}
}

func TestApplyRunsChangesInDependencyOrder(t *testing.T) {
	cfg := writeConfig(t, `
ok("c", "create", { "b" })
ok("b", "update", { "a" })
ok("a")
ok("noop", "no-op")
`)
	if err := New(false, nil).Apply(cfg, true, false); err != nil {
		t.Fatal(err)
	}
	if got, want := appliedIDs(t, cfg), []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("applied = %v, want %v", got, want)
	}
	if got, want := stateIDs(t, cfg), []string{"a", "b", "c", "noop"}; !reflect.DeepEqual(got, want) {
		t.Errorf("state = %v, want %v", got, want)
	}
}

func TestApplySkipsDependentsOfFailedResource(t *testing.T) {
	cfg := writeConfig(t, `
failing("base")
ok("child", "create", { "base" })
ok("grandchild", "create", { "child" })
ok("independent")
`)
	err := New(false, nil).Apply(cfg, true, false)
	if err == nil || !strings.Contains(err.Error(), "test.base: boom") {
		t.Fatalf("expected base failure, got %v", err)
	}
	if got, want := appliedIDs(t, cfg), []string{"independent"}; !reflect.DeepEqual(got, want) {
		t.Errorf("applied = %v, want %v", got, want)
	}
}

func TestApplyPersistsStateOnPartialFailure(t *testing.T) {
	cfg := writeConfig(t, `
ok("good")
failing("bad")
failing("tracked-bad")
`)
	setState(t, cfg, "tracked-bad")
	if err := New(false, nil).Apply(cfg, true, false); err == nil {
		t.Fatal("expected error")
	}
	// "good" applied, "bad" never existed, "tracked-bad" was already tracked.
	if got, want := stateIDs(t, cfg), []string{"good", "tracked-bad"}; !reflect.DeepEqual(got, want) {
		t.Errorf("state = %v, want %v", got, want)
	}
}

func TestApplyPlanErrorAppliesNothing(t *testing.T) {
	cfg := writeConfig(t, `
ok("a")
resource("broken", { type = "test", plan = function() error("cannot plan") end, apply = function() end })
`)
	err := New(false, nil).Apply(cfg, true, false)
	if err == nil || !strings.Contains(err.Error(), "cannot plan") {
		t.Fatalf("expected plan error, got %v", err)
	}
	if got := appliedIDs(t, cfg); len(got) != 0 {
		t.Errorf("applied = %v, want nothing", got)
	}
	if _, err := os.Stat(stateFilePath(cfg)); !os.IsNotExist(err) {
		t.Errorf("state file should not be written on plan error")
	}
}

func TestApplyConfirmationPrompt(t *testing.T) {
	for _, tc := range []struct {
		input   string
		applied bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"n\n", false},
		{"\n", false},
		{"", false},
	} {
		cfg := writeConfig(t, `ok("a")`)
		eng := New(false, nil)
		eng.Stdin = strings.NewReader(tc.input)
		if err := eng.Apply(cfg, false, false); err != nil {
			t.Fatal(err)
		}
		if got := len(appliedIDs(t, cfg)) > 0; got != tc.applied {
			t.Errorf("input %q: applied = %v, want %v", tc.input, got, tc.applied)
		}
	}
}

func TestApplyOrphans(t *testing.T) {
	t.Run("kept without prune", func(t *testing.T) {
		cfg := writeConfig(t, `ok("a", "no-op")`)
		setState(t, cfg, "a", "gone")
		if err := New(false, nil).Apply(cfg, true, false); err != nil {
			t.Fatal(err)
		}
		if got, want := stateIDs(t, cfg), []string{"a", "gone"}; !reflect.DeepEqual(got, want) {
			t.Errorf("state = %v, want %v", got, want)
		}
	})
	t.Run("dropped with prune", func(t *testing.T) {
		cfg := writeConfig(t, `ok("a", "no-op")`)
		setState(t, cfg, "a", "gone")
		if err := New(false, nil).Apply(cfg, true, true); err != nil {
			t.Fatal(err)
		}
		if got, want := stateIDs(t, cfg), []string{"a"}; !reflect.DeepEqual(got, want) {
			t.Errorf("state = %v, want %v", got, want)
		}
	})
	t.Run("pruned alongside changes", func(t *testing.T) {
		cfg := writeConfig(t, `ok("a")`)
		setState(t, cfg, "gone")
		if err := New(false, nil).Apply(cfg, true, true); err != nil {
			t.Fatal(err)
		}
		if got, want := stateIDs(t, cfg), []string{"a"}; !reflect.DeepEqual(got, want) {
			t.Errorf("state = %v, want %v", got, want)
		}
	})
}

func TestPlanReturnsErrorsAndWritesNoState(t *testing.T) {
	cfg := writeConfig(t, `
ok("a")
resource("bad", { type = "test", plan = function() return "nope" end, apply = function() end })
`)
	err := New(false, nil).Plan(cfg)
	if err == nil || !strings.Contains(err.Error(), "plan function must return a table") {
		t.Fatalf("expected plan error, got %v", err)
	}
	if _, err := os.Stat(stateFilePath(cfg)); !os.IsNotExist(err) {
		t.Errorf("plan must not write state")
	}
}

func TestParsePlanResult(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	eval := func(src string) lua.LValue {
		t.Helper()
		if err := L.DoString("return " + src); err != nil {
			t.Fatal(err)
		}
		v := L.Get(-1)
		L.Pop(1)
		return v
	}

	t.Run("non-table", func(t *testing.T) {
		p := parsePlanResult("x", "t", eval(`nil`))
		if p.Change != ChangeError || p.Err == nil {
			t.Errorf("got %+v", p)
		}
	})
	t.Run("error field wins", func(t *testing.T) {
		p := parsePlanResult("x", "t", eval(`{ change = "create", error = "bad" }`))
		if p.Change != ChangeError || p.Err == nil || p.Err.Error() != "bad" {
			t.Errorf("got %+v", p)
		}
	})
	t.Run("diffs", func(t *testing.T) {
		p := parsePlanResult("x", "t", eval(`{ change = "update", diffs = {
			{ field = "a", before = "1", after = "2" },
			"ignored",
			{ field = "b", after = "3" },
		} }`))
		want := []Diff{{Field: "a", Before: "1", After: "2"}, {Field: "b", After: "3"}}
		if p.Change != ChangeUpdate || !reflect.DeepEqual(p.Diffs, want) {
			t.Errorf("got %+v", p)
		}
	})
	t.Run("diffs not a table", func(t *testing.T) {
		p := parsePlanResult("x", "t", eval(`{ change = "update", diffs = "oops" }`))
		if p.Change != ChangeUpdate || len(p.Diffs) != 0 {
			t.Errorf("got %+v", p)
		}
	})
}

func TestSecretDefaultsAreRedacted(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "infra.lua")
	if err := os.WriteFile(cfg, []byte(`local pw = secret("db", "fallback-pw")`), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := luaruntime.Eval(cfg, false, map[string]string{"api": "from-store"})
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	got := redactSecrets("db=fallback-pw api=from-store", New(false, map[string]string{"api": "from-store"}).redactions(reg))
	if got != "db=[REDACTED] api=[REDACTED]" {
		t.Errorf("got %q", got)
	}
}
