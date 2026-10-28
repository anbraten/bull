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
	Orphaned  bool // in state but no longer in config
}

// StateResource is a resource entry persisted in the state file.
type StateResource struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// State is the persisted state of applied resources.
type State struct {
	Resources []StateResource `json:"resources"`
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
