package engine

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	luaruntime "github.com/anbraten/bull/internal/lua"
	lua "github.com/yuin/gopher-lua"
)

// Engine orchestrates plan and apply.
type Engine struct {
	Verbose bool
}

func New(verbose bool) *Engine { return &Engine{Verbose: verbose} }

func (e *Engine) Plan(filePath string) error {
	reg, err := luaruntime.Eval(filePath)
	if err != nil {
		return err
	}
	defer reg.Close()
	defer reg.Pool().Close()

	plans := planAll(reg)
	printPlans(plans)
	return firstPlanError(plans)
}

func (e *Engine) Apply(filePath string, autoApprove bool) error {
	reg, err := luaruntime.Eval(filePath)
	if err != nil {
		return err
	}
	defer reg.Close()
	defer reg.Pool().Close()

	plans := planAll(reg)
	printPlans(plans)

	if err := firstPlanError(plans); err != nil {
		return err
	}

	changes := filterChanges(plans)
	if len(changes) == 0 {
		fmt.Println("\nNothing to apply.")
		return nil
	}

	if !autoApprove {
		fmt.Printf("\nApply %d change(s)? [yes/no]: ", len(changes))
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(line)) != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	return applyAll(reg, changes)
}

func (e *Engine) Validate(filePath string) error {
	reg, err := luaruntime.Eval(filePath)
	if err != nil {
		return err
	}
	defer reg.Close()
	defer reg.Pool().Close()

	// TODO: check dependsOn references and cycles before calling plan functions?

	sorted, err := topoSort(reg.Resources)
	if err != nil {
		return err
	}

	printValidate(sorted)
	return nil
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
// Resources whose dependencies failed are skipped.
func applyAll(reg *luaruntime.Registry, plans []ResourcePlan) error {
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
		return fmt.Errorf("%d resource(s) failed:\n  %s", len(errs), strings.Join(errs, "\n  "))
	}
	return nil
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
