package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	luaruntime "github.com/anbraten/bull/internal/lua"
)

// stateFilePath returns the state file path derived from the config file path.
// e.g. "infra.lua" → "infra.state.json"
func stateFilePath(configFile string) string {
	dir := filepath.Dir(configFile)
	base := strings.TrimSuffix(filepath.Base(configFile), filepath.Ext(configFile))
	return filepath.Join(dir, base+".state.json")
}

func loadState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read state: %w", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("parse state: %w", err)
	}
	return state, nil
}

func writeState(path string, state State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// orphanedPlans returns delete plans for resources present in state but absent from the current registry.
func orphanedPlans(state State, reg *luaruntime.Registry) []ResourcePlan {
	currentIDs := make(map[string]bool, len(reg.Resources))
	for _, r := range reg.Resources {
		currentIDs[r.ID] = true
	}
	var plans []ResourcePlan
	for _, sr := range state.Resources {
		if !currentIDs[sr.ID] {
			plans = append(plans, ResourcePlan{
				ID:       sr.ID,
				Type:     sr.Type,
				Change:   ChangeDelete,
				Orphaned: true,
			})
		}
	}
	return plans
}

// buildState constructs the state to persist after an apply.
// Current resources are included unless they failed (or were skipped) and
// weren't already tracked. Orphaned resources are kept unless prune is set.
func buildState(reg *luaruntime.Registry, prev State, orphans []ResourcePlan, prune bool, failed map[string]bool) State {
	tracked := make(map[string]bool, len(prev.Resources))
	for _, sr := range prev.Resources {
		tracked[sr.ID] = true
	}
	var resources []StateResource
	for _, r := range reg.Resources {
		if failed[r.ID] && !tracked[r.ID] {
			continue
		}
		resources = append(resources, StateResource{ID: r.ID, Type: r.Type})
	}
	if !prune {
		for _, o := range orphans {
			resources = append(resources, StateResource{ID: o.ID, Type: o.Type})
		}
	}
	return State{Resources: resources}
}
