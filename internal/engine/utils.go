package engine

import (
	"fmt"
	"sort"
	"strings"

	luaruntime "github.com/anbraten/bull/internal/lua"
)

// stripLuaTrace removes Lua location prefixes and stack traces from errors.
// Input:  "/path/to/file.lua:42: some error\nstack traceback:\n\t..."
// Output: "some error"
func stripLuaTrace(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()

	// Remove stack traceback block
	if idx := strings.Index(msg, "\nstack traceback:"); idx >= 0 {
		msg = strings.TrimRight(msg[:idx], " \t")
	}

	// Remove leading location prefix: "<string>:NN: " or "/path/file.lua:NN: "
	// A location prefix ends at the second colon (after the line number).
	if idx := strings.Index(msg, ":"); idx >= 0 {
		rest := msg[idx+1:] // after first colon
		if before, after, ok := strings.Cut(rest, ": "); ok {
			// check that the part between colons is a line number
			lineNum := before
			isNum := len(lineNum) > 0
			for _, c := range lineNum {
				if c < '0' || c > '9' {
					isNum = false
					break
				}
			}
			if isNum {
				msg = after
			}
		}
	}

	return fmt.Errorf("%s", msg)
}

// topoSort returns resources in dependency order, preserving declaration order
// for resources at the same level. Returns an error for unknown deps or cycles.
func topoSort(resources []*luaruntime.Resource) ([]*luaruntime.Resource, error) {
	idx := make(map[string]int, len(resources))
	for i, r := range resources {
		idx[r.ID] = i
	}

	for _, r := range resources {
		for _, dep := range r.DependsOn {
			if _, ok := idx[dep]; !ok {
				return nil, fmt.Errorf("resource %q depends on unknown resource %q", r.ID, dep)
			}
		}
	}

	// adj[i] = declaration indices of resources that depend on resources[i]
	adj := make([][]int, len(resources))
	inDegree := make([]int, len(resources))
	for i, r := range resources {
		for _, dep := range r.DependsOn {
			j := idx[dep]
			adj[j] = append(adj[j], i)
			inDegree[i]++
		}
	}

	// Seed queue with zero-in-degree nodes in declaration order
	var queue []int
	for i := range resources {
		if inDegree[i] == 0 {
			queue = append(queue, i)
		}
	}

	result := make([]*luaruntime.Resource, 0, len(resources))
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		result = append(result, resources[cur])

		var freed []int
		for _, dep := range adj[cur] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				freed = append(freed, dep)
			}
		}
		sort.Ints(freed) // sort by declaration index to keep stable order
		queue = append(queue, freed...)
	}

	if len(result) != len(resources) {
		var cycle []string
		for i, r := range resources {
			if inDegree[i] > 0 {
				cycle = append(cycle, r.ID)
			}
		}
		return nil, fmt.Errorf("dependency cycle detected among: %s", strings.Join(cycle, ", "))
	}
	return result, nil
}
