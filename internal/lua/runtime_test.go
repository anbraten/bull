package lua

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "infra.lua")
}

func TestOverrideWarnings(t *testing.T) {
	cfg := writeFiles(t, map[string]string{
		"bull_modules/a.lua": "function file() end\nfunction apt() end",
		"bull_modules/b.lua": "function apt() end\nfunction helper() end",
		"infra.lua":          "function shell() end\nlocal x = 1\nmy_var = 2",
	})
	reg, err := Eval(cfg, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()

	want := []string{
		`bull_modules/a.lua overrides "file" from built-in`,
		`bull_modules/b.lua overrides "apt" from bull_modules/a.lua`,
		`infra.lua overrides "shell" from built-in`,
	}
	if !reflect.DeepEqual(reg.Warnings, want) {
		t.Errorf("warnings =\n  %q\nwant\n  %q", reg.Warnings, want)
	}
}

func TestNoWarningsForNewGlobals(t *testing.T) {
	cfg := writeFiles(t, map[string]string{
		"bull_modules/apt.lua": "function apt() end",
		"infra.lua":            "function mark() end\ncount = 1",
	})
	reg, err := Eval(cfg, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	if len(reg.Warnings) != 0 {
		t.Errorf("unexpected warnings: %q", reg.Warnings)
	}
}
