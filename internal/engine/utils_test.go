package engine

import (
	"fmt"
	"testing"

	luaruntime "github.com/anbraten/bull/internal/lua"
)

func TestStripLuaTraceNil(t *testing.T) {
	got := stripLuaTrace(nil)
	if got != nil {
		t.Errorf("stripLuaTrace(nil) = %v, want nil", got)
	}
}

func TestStripLuaTraceSimpleError(t *testing.T) {
	err := fmt.Errorf("simple error")
	got := stripLuaTrace(err)
	if got.Error() != "simple error" {
		t.Errorf("stripLuaTrace() = %q, want %q", got.Error(), "simple error")
	}
}

func TestStripLuaTraceWithLocation(t *testing.T) {
	err := fmt.Errorf("infra.lua:42: something went wrong")
	got := stripLuaTrace(err)
	if got.Error() != "something went wrong" {
		t.Errorf("stripLuaTrace() = %q, want %q", got.Error(), "something went wrong")
	}
}

func TestStripLuaTraceWithStackTrace(t *testing.T) {
	err := fmt.Errorf("config.lua:10: an error\nstack traceback:\n\t[C]: in function\n\tconfig.lua:10: in main chunk")
	got := stripLuaTrace(err)
	if got.Error() != "an error" {
		t.Errorf("stripLuaTrace() = %q, want %q", got.Error(), "an error")
	}
}

func TestStripLuaTraceStringPrefix(t *testing.T) {
	err := fmt.Errorf("<string>:5: failed")
	got := stripLuaTrace(err)
	if got.Error() != "failed" {
		t.Errorf("stripLuaTrace() = %q, want %q", got.Error(), "failed")
	}
}

func TestStripLuaTraceAbsolutePath(t *testing.T) {
	err := fmt.Errorf("/home/user/infra.lua:99: error message")
	got := stripLuaTrace(err)
	if got.Error() != "error message" {
		t.Errorf("stripLuaTrace() = %q, want %q", got.Error(), "error message")
	}
}

func TestStripLuaTracePreservesColonsInMessage(t *testing.T) {
	err := fmt.Errorf("script.lua:1: error: reason: explanation")
	got := stripLuaTrace(err)
	if got.Error() != "error: reason: explanation" {
		t.Errorf("stripLuaTrace() = %q, want %q", got.Error(), "error: reason: explanation")
	}
}

func TestTopoSortNoResources(t *testing.T) {
	result, err := topoSort([]*luaruntime.Resource{})
	if err != nil {
		t.Fatalf("topoSort([]) should not error, got %v", err)
	}
	if len(result) != 0 {
		t.Errorf("topoSort([]) = %v, want empty", result)
	}
}

func TestTopoSortSingleResource(t *testing.T) {
	r1 := &luaruntime.Resource{ID: "a"}
	result, err := topoSort([]*luaruntime.Resource{r1})
	if err != nil {
		t.Fatalf("topoSort([a]) error = %v", err)
	}
	if len(result) != 1 || result[0].ID != "a" {
		t.Errorf("topoSort([a]) = %v, want [a]", result)
	}
}

func TestTopoSortNoDependencies(t *testing.T) {
	r1 := &luaruntime.Resource{ID: "a"}
	r2 := &luaruntime.Resource{ID: "b"}
	r3 := &luaruntime.Resource{ID: "c"}

	result, err := topoSort([]*luaruntime.Resource{r1, r2, r3})
	if err != nil {
		t.Fatalf("topoSort error = %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("topoSort returned %d resources, want 3", len(result))
	}
	// Should preserve declaration order
	if result[0].ID != "a" || result[1].ID != "b" || result[2].ID != "c" {
		t.Errorf("topoSort() = [%s, %s, %s], want [a, b, c]",
			result[0].ID, result[1].ID, result[2].ID)
	}
}

func TestTopoSortSimpleDependency(t *testing.T) {
	r1 := &luaruntime.Resource{ID: "a"}
	r2 := &luaruntime.Resource{ID: "b", DependsOn: []string{"a"}}

	result, err := topoSort([]*luaruntime.Resource{r1, r2})
	if err != nil {
		t.Fatalf("topoSort error = %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("topoSort returned %d resources, want 2", len(result))
	}
	if result[0].ID != "a" || result[1].ID != "b" {
		t.Errorf("topoSort() = [%s, %s], want [a, b]", result[0].ID, result[1].ID)
	}
}

func TestTopoSortReverse(t *testing.T) {
	r1 := &luaruntime.Resource{ID: "a", DependsOn: []string{"b"}}
	r2 := &luaruntime.Resource{ID: "b"}

	result, err := topoSort([]*luaruntime.Resource{r1, r2})
	if err != nil {
		t.Fatalf("topoSort error = %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("topoSort returned %d resources, want 2", len(result))
	}
	// b must come before a
	if result[0].ID != "b" || result[1].ID != "a" {
		t.Errorf("topoSort() = [%s, %s], want [b, a]", result[0].ID, result[1].ID)
	}
}

func TestTopoSortChain(t *testing.T) {
	r1 := &luaruntime.Resource{ID: "a"}
	r2 := &luaruntime.Resource{ID: "b", DependsOn: []string{"a"}}
	r3 := &luaruntime.Resource{ID: "c", DependsOn: []string{"b"}}

	result, err := topoSort([]*luaruntime.Resource{r1, r2, r3})
	if err != nil {
		t.Fatalf("topoSort error = %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("topoSort returned %d resources, want 3", len(result))
	}
	expected := []string{"a", "b", "c"}
	for i, exp := range expected {
		if result[i].ID != exp {
			t.Errorf("topoSort()[%d] = %s, want %s", i, result[i].ID, exp)
		}
	}
}

func TestTopoSortMultipleDeps(t *testing.T) {
	r1 := &luaruntime.Resource{ID: "a"}
	r2 := &luaruntime.Resource{ID: "b"}
	r3 := &luaruntime.Resource{ID: "c", DependsOn: []string{"a", "b"}}

	result, err := topoSort([]*luaruntime.Resource{r1, r2, r3})
	if err != nil {
		t.Fatalf("topoSort error = %v", err)
	}
	// a and b must come before c
	found := make(map[string]int)
	for i, r := range result {
		found[r.ID] = i
	}
	if found["a"] > found["c"] || found["b"] > found["c"] {
		t.Errorf("topoSort() violates dependency: a,b must come before c")
	}
}

func TestTopoSortUnknownDependency(t *testing.T) {
	r1 := &luaruntime.Resource{ID: "a", DependsOn: []string{"unknown"}}

	_, err := topoSort([]*luaruntime.Resource{r1})
	if err == nil {
		t.Fatalf("topoSort should error on unknown dependency, got nil")
	}
	if err.Error() != "resource \"a\" depends on unknown resource \"unknown\"" {
		t.Errorf("topoSort() error = %q, want dependency error", err.Error())
	}
}

func TestTopoSortCycle(t *testing.T) {
	r1 := &luaruntime.Resource{ID: "a", DependsOn: []string{"b"}}
	r2 := &luaruntime.Resource{ID: "b", DependsOn: []string{"a"}}

	_, err := topoSort([]*luaruntime.Resource{r1, r2})
	if err == nil {
		t.Fatalf("topoSort should detect cycle, got nil")
	}
	if err.Error() != "dependency cycle detected among: a, b" {
		t.Errorf("topoSort() error = %q, want cycle error", err.Error())
	}
}

func TestTopoSortComplexCycle(t *testing.T) {
	r1 := &luaruntime.Resource{ID: "a", DependsOn: []string{"b"}}
	r2 := &luaruntime.Resource{ID: "b", DependsOn: []string{"c"}}
	r3 := &luaruntime.Resource{ID: "c", DependsOn: []string{"a"}}

	_, err := topoSort([]*luaruntime.Resource{r1, r2, r3})
	if err == nil {
		t.Fatalf("topoSort should detect cycle")
	}
	if err.Error() != "dependency cycle detected among: a, b, c" {
		t.Errorf("topoSort() error = %q", err.Error())
	}
}

func TestTopoSortPreserveDeclarationOrder(t *testing.T) {
	// When two resources have no ordering constraint between them,
	// the declaration order should be preserved
	r1 := &luaruntime.Resource{ID: "c"}
	r2 := &luaruntime.Resource{ID: "a"}
	r3 := &luaruntime.Resource{ID: "b"}

	result, err := topoSort([]*luaruntime.Resource{r1, r2, r3})
	if err != nil {
		t.Fatalf("topoSort error = %v", err)
	}
	if result[0].ID != "c" || result[1].ID != "a" || result[2].ID != "b" {
		t.Errorf("topoSort() = [%s, %s, %s], want [c, a, b]",
			result[0].ID, result[1].ID, result[2].ID)
	}
}

func TestTopoSortDependencyPreemptsDeclOrder(t *testing.T) {
	// With dependencies, ordering constraints should beat declaration order
	r1 := &luaruntime.Resource{ID: "z"}
	r2 := &luaruntime.Resource{ID: "a", DependsOn: []string{"z"}}

	result, err := topoSort([]*luaruntime.Resource{r1, r2})
	if err != nil {
		t.Fatalf("topoSort error = %v", err)
	}
	if result[0].ID != "z" || result[1].ID != "a" {
		t.Errorf("topoSort() = [%s, %s], want [z, a]",
			result[0].ID, result[1].ID)
	}
}

func TestTopoSortDiamondPattern(t *testing.T) {
	// Diamond: both c and d depend on a and b
	r1 := &luaruntime.Resource{ID: "a"}
	r2 := &luaruntime.Resource{ID: "b"}
	r3 := &luaruntime.Resource{ID: "c", DependsOn: []string{"a", "b"}}
	r4 := &luaruntime.Resource{ID: "d", DependsOn: []string{"a", "b"}}

	result, err := topoSort([]*luaruntime.Resource{r1, r2, r3, r4})
	if err != nil {
		t.Fatalf("topoSort error = %v", err)
	}

	found := make(map[string]int)
	for i, r := range result {
		found[r.ID] = i
	}

	// Verify ordering constraints
	if found["a"] > found["c"] || found["b"] > found["c"] {
		t.Errorf("topoSort() violates c's dependencies")
	}
	if found["a"] > found["d"] || found["b"] > found["d"] {
		t.Errorf("topoSort() violates d's dependencies")
	}
}
