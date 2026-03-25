package engine

import "fmt"

type ChangeType string

const (
	ChangeCreate ChangeType = "create"
	ChangeUpdate ChangeType = "update"
	ChangeDelete ChangeType = "delete"
	ChangeNoOp   ChangeType = "no-op"
	ChangeError  ChangeType = "error"
)

type Diff struct {
	Field  string
	Before string
	After  string
}

type ResourcePlan struct {
	ID        string
	Type      string
	Change    ChangeType
	Diffs     []Diff
	Err       error
	DependsOn []string
}

func (p ResourcePlan) HasChanges() bool {
	return p.Change != ChangeNoOp && p.Change != ChangeError
}

func (p ResourcePlan) FormatID() string {
	if p.Type == "" {
		return p.ID
	}
	return fmt.Sprintf("%s.%s", p.Type, p.ID)
}
