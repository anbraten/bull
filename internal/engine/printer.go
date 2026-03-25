package engine

import (
	"fmt"

	luaruntime "github.com/anbraten/bull/internal/lua"
	"github.com/fatih/color"
)

var (
	green  = color.New(color.FgGreen).SprintFunc()
	yellow = color.New(color.FgYellow).SprintFunc()
	red    = color.New(color.FgRed).SprintFunc()
	cyan   = color.New(color.FgCyan).SprintFunc()
	bold   = color.New(color.Bold).SprintFunc()
	faint  = color.New(color.Faint).SprintFunc()
)

func isSecretValue(val string, secrets map[string]string) bool {
	for _, v := range secrets {
		if val == v {
			return true
		}
	}
	return false
}

func redactIfSecret(val string, secrets map[string]string) string {
	if isSecretValue(val, secrets) {
		return "[REDACTED]"
	}
	return val
}

func printPlans(plans []ResourcePlan, secrets map[string]string) {
	creates, updates, noop, errors := 0, 0, 0, 0
	for _, p := range plans {
		switch {
		case p.Err != nil || p.Change == ChangeError:
			errors++
		case p.Change == ChangeCreate:
			creates++
		case p.Change == ChangeUpdate:
			updates++
		default:
			noop++
		}
	}

	fmt.Printf("%s\n\n", bold("Plan"))

	for _, p := range plans {
		id := p.FormatID()
		switch {
		case p.Err != nil || p.Change == ChangeError:
			fmt.Printf("  %s %s\n", red("!"), bold(id))
			if p.Err != nil {
				fmt.Printf("      %s\n", red(p.Err.Error()))
			}

		case p.Change == ChangeNoOp:
			fmt.Printf("  %s %s\n", faint("·"), faint(id))

		case p.Change == ChangeCreate:
			fmt.Printf("  %s %s\n", green("+"), bold(id))
			for _, d := range p.Diffs {
				after := redactIfSecret(d.After, secrets)
				fmt.Printf("      %s %s = %s\n", green("+"), cyan(d.Field), green(after))
			}

		case p.Change == ChangeUpdate:
			fmt.Printf("  %s %s\n", yellow("~"), bold(id))
			for _, d := range p.Diffs {
				before := redactIfSecret(d.Before, secrets)
				after := redactIfSecret(d.After, secrets)
				if d.Before == "" {
					fmt.Printf("      %s %s = %s\n", green("+"), cyan(d.Field), green(after))
				} else if d.After == "" {
					fmt.Printf("      %s %s = %s\n", red("-"), cyan(d.Field), red(before))
				} else {
					fmt.Printf("      %s %s: %s → %s\n",
						yellow("~"), cyan(d.Field), red(before), green(after))
				}
			}

		case p.Change == ChangeDelete:
			fmt.Printf("  %s %s\n", red("-"), bold(id))
		}
	}

	fmt.Println()
	fmt.Printf("  %s  %s  %s  %s\n",
		green(fmt.Sprintf("%d to create", creates)),
		yellow(fmt.Sprintf("%d to update", updates)),
		faint(fmt.Sprintf("%d unchanged", noop)),
		errorSummary(errors),
	)
}

func errorSummary(n int) string {
	if n == 0 {
		return faint("0 errors")
	}
	return red(fmt.Sprintf("%d error(s)", n))
}

func printApplying(p ResourcePlan) {
	verb := "Creating"
	if p.Change == ChangeUpdate {
		verb = "Updating"
	} else if p.Change == ChangeDelete {
		verb = "Deleting"
	}
	fmt.Printf("  %s %s...\n", yellow(verb), bold(p.FormatID()))
}

func printApplyDone(id, resType string) {
	fmt.Printf("  %s %s\n", green("✓"), bold(resType+"."+id))
}

func printApplyError(id, resType string, err error) {
	fmt.Printf("  %s %s: %v\n", red("✗"), bold(resType+"."+id), err)
}

func printApplySkipped(id, resType, failedDep string) {
	fmt.Printf("  %s %s (skipped: %s failed)\n", yellow("–"), bold(resType+"."+id), failedDep)
}

func printValidate(resources []*luaruntime.Resource) {
	fmt.Printf("%s\n", bold("Validate"))
	for _, r := range resources {
		fmt.Printf("  %s %s\n", green("✓"), bold(r.Type+"."+r.ID))
	}
}
