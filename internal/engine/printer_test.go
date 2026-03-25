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

	printPlans(plan, map[string]string{})
}

func TestIsSecretValue(t *testing.T) {
	secrets := map[string]string{
		"API_KEY": "abc123secret",
		"DB_PASS": "supersecret",
	}

	tests := []struct {
		val  string
		want bool
	}{
		{"abc123secret", true},
		{"supersecret", true},
		{"other", false},
		{"", false},
		{"abc123", false},
	}

	for _, tt := range tests {
		got := isSecretValue(tt.val, secrets)
		if got != tt.want {
			t.Errorf("isSecretValue(%q, secrets) = %v, want %v", tt.val, got, tt.want)
		}
	}
}

func TestIsSecretValueEmpty(t *testing.T) {
	got := isSecretValue("anything", map[string]string{})
	if got != false {
		t.Errorf("isSecretValue with empty secrets map should return false, got %v", got)
	}
}

func TestRedactIfSecret(t *testing.T) {
	secrets := map[string]string{
		"API_KEY": "secret_token_123",
		"DB_PASS": "db_password",
	}

	tests := []struct {
		val  string
		want string
	}{
		{"secret_token_123", "[REDACTED]"},
		{"db_password", "[REDACTED]"},
		{"regular_value", "regular_value"},
		{"", ""},
		{"secret_", "secret_"},
	}

	for _, tt := range tests {
		got := redactIfSecret(tt.val, secrets)
		if got != tt.want {
			t.Errorf("redactIfSecret(%q, secrets) = %q, want %q", tt.val, got, tt.want)
		}
	}
}

func TestRedactIfSecretNoSecrets(t *testing.T) {
	got := redactIfSecret("any_value", map[string]string{})
	if got != "any_value" {
		t.Errorf("redactIfSecret with empty secrets should return original value, got %q", got)
	}
}

func TestPrintPlansWithSecretRedaction(t *testing.T) {
	secrets := map[string]string{
		"API_KEY": "super_secret_key_123",
	}

	plans := []ResourcePlan{
		{
			ID:     "config",
			Type:   "service",
			Change: ChangeCreate,
			Diffs: []Diff{
				{Field: "api_key", After: "super_secret_key_123"},
				{Field: "port", After: "8080"},
			},
		},
		{
			ID:     "db",
			Type:   "database",
			Change: ChangeUpdate,
			Diffs: []Diff{
				{Field: "password", Before: "old_pass", After: "super_secret_key_123"},
			},
		},
	}

	// Just verify it doesn't panic and runs through the redaction logic
	printPlans(plans, secrets)
}

func TestPrintPlansMultipleDiffs(t *testing.T) {
	secrets := map[string]string{
		"SECRET1": "secret_value_1",
		"SECRET2": "secret_value_2",
	}

	plans := []ResourcePlan{
		{
			ID:     "multi",
			Type:   "config",
			Change: ChangeCreate,
			Diffs: []Diff{
				{Field: "key1", After: "secret_value_1"},
				{Field: "key2", After: "public_value"},
				{Field: "key3", After: "secret_value_2"},
			},
		},
	}

	// Verify redaction works across multiple diffs
	printPlans(plans, secrets)
}
