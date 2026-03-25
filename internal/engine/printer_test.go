package engine

import (
	"fmt"
	"testing"
)

func TestPrintPlans(t *testing.T) {
	plan := []ResourcePlan{
		{ID: "a", Type: "res", Change: ChangeCreate, Diffs: []Diff{{Field: "x", After: "1"}}},
		{ID: "b", Type: "res", Change: ChangeUpdate, Diffs: []Diff{{Field: "y", Before: "2", After: "3"}}},
		{ID: "c", Type: "res", Change: ChangeNoOp},
		{ID: "d", Type: "res", Change: ChangeError, Err: fmt.Errorf("something went wrong")},
	}

	printPlans(plan)
}
