package engine

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	luaruntime "github.com/anbraten/bull/internal/lua"
	lua "github.com/yuin/gopher-lua"
)

// Engine orchestrates plan and apply.
type Engine struct {
	Verbose  bool
	FullDiff bool // show complete diffs: no preview cutoff, no size limit
	Secrets  map[string]string
	Stdin    io.Reader // source for the apply confirmation prompt
}

func New(verbose bool, secrets map[string]string) *Engine {
	if secrets == nil {
		secrets = map[string]string{}
	}
	return &Engine{Verbose: verbose, Secrets: secrets, Stdin: os.Stdin}
}

func (e *Engine) Plan(filePath string) error {
	reg, err := e.eval(filePath)
	if err != nil {
		return err
	}
	defer reg.Close()
	defer reg.Pool().Close()

	statePath := stateFilePath(filePath)
	state, err := loadState(statePath)
	if err != nil {
		return err
	}

	plans := planAll(reg)
	plans = append(plans, orphanedPlans(state, reg)...)
	printPlans(plans, e.redactions(reg), e.FullDiff)
	return firstPlanError(plans)
}

func (e *Engine) Apply(filePath string, autoApprove bool, prune bool) error {
	reg, err := e.eval(filePath)
	if err != nil {
		return err
	}
	defer reg.Close()
	defer reg.Pool().Close()

	statePath := stateFilePath(filePath)
	state, err := loadState(statePath)
	if err != nil {
		return err
	}

	plans := planAll(reg)
	orphans := orphanedPlans(state, reg)
	printPlans(append(plans, orphans...), e.redactions(reg), e.FullDiff)

	if err := firstPlanError(plans); err != nil {
		return err
	}

	changes := filterChanges(plans)
	if len(changes) == 0 && len(orphans) == 0 {
		fmt.Println("\nNothing to apply.")
		return writeState(statePath, buildState(reg, state, orphans, prune, nil))
	}

	if len(orphans) > 0 && !prune {
		fmt.Printf("\n%s %d orphaned resource(s) cannot be auto-deleted (use --prune to remove from state).\n",
			yellow("!"), len(orphans))
	}

	if len(changes) == 0 {
		if prune && len(orphans) > 0 {
			fmt.Printf("\nRemoving %d orphaned resource(s) from state.\n", len(orphans))
		} else {
			fmt.Println("\nNothing to apply.")
		}
		return writeState(statePath, buildState(reg, state, orphans, prune, nil))
	}

	if !autoApprove {
		fmt.Printf("\nApply %d change(s)? [y/N]: ", len(changes))
		reader := bufio.NewReader(e.Stdin)
		line, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(line)) != "y" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Persist state even when some resources failed, so everything that did
	// apply is tracked (and can be detected as orphaned later).
	failed, applyErr := applyAll(reg, changes)
	if err := writeState(statePath, buildState(reg, state, orphans, prune, failed)); err != nil {
		return errors.Join(applyErr, err)
	}
	return applyErr
}

func (e *Engine) Validate(filePath string) error {
	reg, err := luaruntime.Eval(filePath, e.Verbose, e.Secrets)
	if err != nil {
		return err
	}
	defer reg.Close()
	defer reg.Pool().Close()

	sorted, err := topoSort(reg.Resources)
	if err != nil {
		return err
	}

	printValidate(sorted)
	return nil
}

// eval evaluates the config and applies engine-level settings to the Lua state.
func (e *Engine) eval(filePath string) (*luaruntime.Registry, error) {
	reg, err := luaruntime.Eval(filePath, e.Verbose, e.Secrets)
	if err != nil {
		return nil, err
	}
	if e.FullDiff {
		// Plan functions read this at call time; lift the text diff size limit.
		reg.L.SetGlobal("_MAX_TEXT_DIFF", lua.LNumber(math.Inf(1)))
	}
	return reg, nil
}

// redactions returns every secret value that must not appear in output:
// the loaded secrets plus whatever secret() returned (including defaults).
func (e *Engine) redactions(reg *luaruntime.Registry) map[string]string {
	out := make(map[string]string, len(e.Secrets)+len(reg.SecretValues))
	for k, v := range e.Secrets {
		out[k] = v
	}
	for k, v := range reg.SecretValues {
		out["used:"+k] = v
	}
	return out
}

// planAll sorts resources into dependency order, then calls each plan function.
func planAll(reg *luaruntime.Registry) []ResourcePlan {
	sorted, err := topoSort(reg.Resources)
	if err != nil {
		return []ResourcePlan{{
			ID: "__deps__", Type: "bull", Change: ChangeError, Err: err,
		}}
	}

	plans := make([]ResourcePlan, len(sorted))
	for i, r := range sorted {
		p := callPlan(reg.L, r)
		p.DependsOn = r.DependsOn
		plans[i] = p
	}
	return plans
}

func callPlan(L *lua.LState, r *luaruntime.Resource) ResourcePlan {
	err := L.CallByParam(lua.P{
		Fn:      r.PlanFn,
		NRet:    1,
		Protect: true,
	})
	if err != nil {
		return ResourcePlan{ID: r.ID, Type: r.Type, Change: ChangeError, Err: stripLuaTrace(err)}
	}

	result := L.Get(-1)
	L.Pop(1)

	return parsePlanResult(r.ID, r.Type, result)
}

func parsePlanResult(id, resType string, v lua.LValue) ResourcePlan {
	tbl, ok := v.(*lua.LTable)
	if !ok {
		return ResourcePlan{
			ID: id, Type: resType, Change: ChangeError,
			Err: fmt.Errorf("plan function must return a table, got %T", v),
		}
	}

	change := ChangeType(lua.LVAsString(tbl.RawGetString("change")))
	errMsg := lua.LVAsString(tbl.RawGetString("error"))

	var diffs []Diff
	if diffsVal := tbl.RawGetString("diffs"); diffsVal != lua.LNil {
		if diffsTbl, ok := diffsVal.(*lua.LTable); ok {
			diffsTbl.ForEach(func(_, val lua.LValue) {
				if d, ok := val.(*lua.LTable); ok {
					diffs = append(diffs, Diff{
						Field:  lua.LVAsString(d.RawGetString("field")),
						Before: lua.LVAsString(d.RawGetString("before")),
						After:  lua.LVAsString(d.RawGetString("after")),
						Text:   lua.LVAsBool(d.RawGetString("text")),
					})
				}
			})
		}
	}

	p := ResourcePlan{ID: id, Type: resType, Change: change, Diffs: diffs}
	if errMsg != "" {
		p.Change = ChangeError
		p.Err = fmt.Errorf("%s", errMsg)
	}
	return p
}

// applyAll calls the apply function for each changed resource.
// Resources whose dependencies failed are skipped. Returns the IDs of failed
// and skipped resources.
func applyAll(reg *luaruntime.Registry, plans []ResourcePlan) (map[string]bool, error) {
	L := reg.L
	// Build a map from resource ID to the Resource struct for apply functions
	byID := make(map[string]*luaruntime.Resource, len(reg.Resources))
	for _, r := range reg.Resources {
		byID[r.ID] = r
	}

	failed := make(map[string]bool)
	var errs []string
	for _, p := range plans {
		// Check if any dependency failed
		var skipDep string
		for _, dep := range p.DependsOn {
			if failed[dep] {
				skipDep = dep
				break
			}
		}
		if skipDep != "" {
			failed[p.ID] = true
			printApplySkipped(p.ID, p.Type, skipDep)
			continue
		}

		r := byID[p.ID]
		printApplying(p)

		err := L.CallByParam(lua.P{
			Fn:      r.ApplyFn,
			NRet:    0,
			Protect: true,
		})
		if err != nil {
			err = stripLuaTrace(err)
			failed[p.ID] = true
			errs = append(errs, fmt.Sprintf("%s.%s: %v", p.Type, p.ID, err))
			printApplyError(p.ID, p.Type, err)
		} else {
			printApplyDone(p.ID, p.Type)
		}
	}

	if len(errs) > 0 {
		return failed, fmt.Errorf("%d resource(s) failed:\n  %s", len(errs), strings.Join(errs, "\n  "))
	}
	return failed, nil
}

func filterChanges(plans []ResourcePlan) []ResourcePlan {
	var out []ResourcePlan
	for _, p := range plans {
		if p.Change != ChangeNoOp && p.Change != ChangeError && p.Err == nil {
			out = append(out, p)
		}
	}
	return out
}

func firstPlanError(plans []ResourcePlan) error {
	var errs []string
	for _, p := range plans {
		if p.Err != nil {
			errs = append(errs, fmt.Sprintf("%s.%s: %v", p.Type, p.ID, p.Err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("plan errors:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}
