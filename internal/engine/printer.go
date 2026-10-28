package engine

import (
	"fmt"
	"strings"

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

// resourceLabel formats "type.id" with type in faint and id in bold.
func resourceLabel(p ResourcePlan) string {
	if p.Type == "" {
		return bold(p.ID)
	}
	return faint(p.Type+".") + bold(p.ID)
}

// printDiffTree renders diff lines with tree drawing characters and aligned fields.
func printDiffTree(diffs []Diff, secrets map[string]string, change ChangeType) {
	if len(diffs) == 0 {
		return
	}

	// compute max field width for alignment
	maxW := 0
	for _, d := range diffs {
		if len(d.Field) > maxW {
			maxW = len(d.Field)
		}
	}

	for i, d := range diffs {
		connector := faint("├─")
		if i == len(diffs)-1 {
			connector = faint("└─")
		}
		padding := strings.Repeat(" ", maxW-len(d.Field))
		field := cyan(d.Field) + padding

		switch change {
		case ChangeCreate:
			val := redactIfSecret(d.After, secrets)
			fmt.Printf("    %s %s  %s\n", connector, field, green(val))
		case ChangeUpdate:
			before := redactIfSecret(d.Before, secrets)
			after := redactIfSecret(d.After, secrets)
			if d.Before == "" {
				fmt.Printf("    %s %s  %s\n", connector, field, green(after))
			} else if d.After == "" {
				fmt.Printf("    %s %s  %s\n", connector, field, red(before))
			} else {
				fmt.Printf("    %s %s  %s  %s  %s\n",
					connector, field, red(before), faint("→"), green(after))
			}
		case ChangeDelete:
			val := redactIfSecret(d.Before, secrets)
			fmt.Printf("    %s %s  %s\n", connector, field, red(val))
		}
	}
}

func printPlans(plans []ResourcePlan, secrets map[string]string) {
	creates, updates, deletes, noop, orphaned, errors := 0, 0, 0, 0, 0, 0
	for _, p := range plans {
		switch {
		case p.Err != nil || p.Change == ChangeError:
			errors++
		case p.Change == ChangeCreate:
			creates++
		case p.Change == ChangeUpdate:
			updates++
		case p.Change == ChangeDelete && p.Orphaned:
			orphaned++
		case p.Change == ChangeDelete:
			deletes++
		default:
			noop++
		}
	}

	fmt.Printf("%s\n\n", bold("Plan"))

	prevWasNoop := false
	for _, p := range plans {
		isNoop := p.Change == ChangeNoOp && p.Err == nil

		// blank line to visually separate after a no-op group ends
		if prevWasNoop && !isNoop {
			fmt.Println()
		}
		prevWasNoop = isNoop

		label := resourceLabel(p)

		switch {
		case p.Err != nil || p.Change == ChangeError:
			fmt.Printf("  %s %s\n", red("!"), label)
			if p.Err != nil {
				fmt.Printf("    %s\n", red(p.Err.Error()))
			}
			fmt.Println()

		case p.Change == ChangeNoOp:
			fmt.Printf("  %s %s\n", faint("·"), faint(p.FormatID()))

		case p.Change == ChangeCreate:
			fmt.Printf("  %s %s\n", green("+"), label)
			printDiffTree(p.Diffs, secrets, ChangeCreate)
			fmt.Println()

		case p.Change == ChangeUpdate:
			fmt.Printf("  %s %s\n", yellow("~"), label)
			printDiffTree(p.Diffs, secrets, ChangeUpdate)
			fmt.Println()

		case p.Change == ChangeDelete && p.Orphaned:
			fmt.Printf("  %s %s  %s\n", red("-"), label, faint("(orphaned)"))
			fmt.Println()

		case p.Change == ChangeDelete:
			fmt.Printf("  %s %s\n", red("-"), label)
			printDiffTree(p.Diffs, secrets, ChangeDelete)
			fmt.Println()
		}
	}

	// summary
	fmt.Printf("  %s\n", faint(strings.Repeat("─", 52)))

	stat := func(n int, label string, colorFn func(...any) string) string {
		if n == 0 {
			return faint(fmt.Sprintf("%d %s", n, label))
		}
		return colorFn(fmt.Sprintf("%d %s", n, label))
	}

	parts := []string{
		stat(creates, "to create", func(a ...any) string { return green(a...) }),
		stat(updates, "to update", func(a ...any) string { return yellow(a...) }),
		stat(deletes, "to delete", func(a ...any) string { return red(a...) }),
		faint(fmt.Sprintf("%d unchanged", noop)),
	}
	if errors > 0 {
		parts = append(parts, red(fmt.Sprintf("%d error(s)", errors)))
	}
	if orphaned > 0 {
		parts = append(parts, yellow(fmt.Sprintf("%d orphaned", orphaned)))
	}
	fmt.Printf("  %s\n", strings.Join(parts, faint("  ·  ")))
}

func printApplying(p ResourcePlan) {
	verb := "Creating"
	c := yellow
	if p.Change == ChangeUpdate {
		verb = "Updating"
	} else if p.Change == ChangeDelete {
		verb = "Deleting"
		c = red
	}
	label := resourceLabel(p)
	fmt.Printf("  %s  %s\n", c(verb), label)
}

func printApplyDone(id, resType string) {
	label := faint(resType+".") + bold(id)
	fmt.Printf("  %s  %s\n\n", green("✓"), label)
}

func printApplyError(id, resType string, err error) {
	label := faint(resType+".") + bold(id)
	fmt.Printf("  %s  %s\n    %s\n\n", red("✗"), label, red(err.Error()))
}

func printApplySkipped(id, resType, failedDep string) {
	label := faint(resType+".") + bold(id)
	fmt.Printf("  %s  %s  %s\n\n", faint("–"), label, faint("(skipped: "+failedDep+" failed)"))
}

func printValidate(resources []*luaruntime.Resource) {
	fmt.Printf("%s\n", bold("Validate"))
	for _, r := range resources {
		label := faint(r.Type+".") + bold(r.ID)
		fmt.Printf("  %s  %s\n", green("✓"), label)
	}
}
