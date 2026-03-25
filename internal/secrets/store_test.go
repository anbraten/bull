package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".bull.secrets.enc")
	key := "test-key"

	in := map[string]string{
		"API_TOKEN": "abc123",
		"DB_PASS":   "secret",
	}
	if err := Save(path, key, in); err != nil {
		t.Fatalf("save: %v", err)
	}

	out, err := Load(path, key)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if out["API_TOKEN"] != in["API_TOKEN"] || out["DB_PASS"] != in["DB_PASS"] {
		t.Fatalf("values mismatch: got=%v want=%v", out, in)
	}
}

func TestLoadMissingFile(t *testing.T) {
	values, err := Load(filepath.Join(t.TempDir(), "missing.enc"), "any")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(values) != 0 {
		t.Fatalf("expected empty map, got %v", values)
	}
}

func TestLoadRequiresKeyWhenFileExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.enc")
	if err := os.WriteFile(path, []byte(fileHeader+"Zm9v\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := Load(path, "")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestLoadWithWrongKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.enc")
	key := "correct-key"

	in := map[string]string{"SECRET": "value"}
	if err := Save(path, key, in); err != nil {
		t.Fatalf("save: %v", err)
	}

	_, err := Load(path, "wrong-key")
	if err == nil {
		t.Fatalf("expected error when loading with wrong key")
	}
}

func TestSaveWithEmptySecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.enc")
	key := "test-key"

	if err := Save(path, key, map[string]string{}); err != nil {
		t.Fatalf("save empty map: %v", err)
	}

	out, err := Load(path, key)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected empty map, got %v", out)
	}
}

func TestSaveWithoutKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.enc")

	err := Save(path, "", map[string]string{"KEY": "value"})
	if err == nil {
		t.Fatalf("expected error when saving without key")
	}
}

func TestMultipleRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.enc")
	key := "test-key"

	// First save
	first := map[string]string{
		"KEY1": "value1",
		"KEY2": "value2",
	}
	if err := Save(path, key, first); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Load and verify
	loaded1, err := Load(path, key)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if loaded1["KEY1"] != "value1" {
		t.Fatalf("first load mismatch")
	}

	// Save different values
	second := map[string]string{
		"KEY1": "updated1",
		"KEY3": "value3",
	}
	if err := Save(path, key, second); err != nil {
		t.Fatalf("second save: %v", err)
	}

	// Load and verify
	loaded2, err := Load(path, key)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if loaded2["KEY1"] != "updated1" || loaded2["KEY3"] != "value3" {
		t.Fatalf("second load mismatch")
	}
	if _, ok := loaded2["KEY2"]; ok {
		t.Fatalf("KEY2 should not exist after second save")
	}
}

func TestParseEnv(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    map[string]string
		wantErr bool
	}{
		{
			name:    "simple",
			content: "KEY=value\n",
			want:    map[string]string{"KEY": "value"},
		},
		{
			name:    "multiple",
			content: "KEY1=value1\nKEY2=value2\n",
			want:    map[string]string{"KEY1": "value1", "KEY2": "value2"},
		},
		{
			name:    "with comments",
			content: "# Comment\nKEY=value\n# Another comment\n",
			want:    map[string]string{"KEY": "value"},
		},
		{
			name:    "with empty lines",
			content: "\nKEY=value\n\n",
			want:    map[string]string{"KEY": "value"},
		},
		{
			name:    "with spaces",
			content: "  KEY  =  value  \n",
			want:    map[string]string{"KEY": "value"},
		},
		{
			name:    "invalid no equals",
			content: "INVALID\n",
			wantErr: true,
		},
		{
			name:    "invalid empty key",
			content: "=value\n",
			wantErr: true,
		},
		{
			name:    "value with equals",
			content: "KEY=value=with=equals\n",
			want:    map[string]string{"KEY": "value=with=equals"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEnv(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseEnv() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("parseEnv() got %d keys, want %d", len(got), len(tt.want))
				return
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("parseEnv() got[%s] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestRenderEnv(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
		want   string
	}{
		{
			name:   "empty",
			values: map[string]string{},
			want:   "# KEY=value\n",
		},
		{
			name:   "single",
			values: map[string]string{"KEY": "value"},
			want:   "KEY=value\n",
		},
		{
			name:   "multiple sorted",
			values: map[string]string{"Z": "last", "A": "first", "M": "middle"},
			want:   "A=first\nM=middle\nZ=last\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderEnv(tt.values)
			if got != tt.want {
				t.Errorf("renderEnv() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePathAbsolute(t *testing.T) {
	got, err := ResolvePath("any/config.lua", "/absolute/path/secrets.enc")
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if got != "/absolute/path/secrets.enc" {
		t.Errorf("ResolvePath() = %q, want /absolute/path/secrets.enc", got)
	}
}

func TestResolvePathRelative(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "infra.lua")
	secretsFile := ".bull.secrets.enc"

	got, err := ResolvePath(configPath, secretsFile)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}

	expected := filepath.Join(dir, secretsFile)
	if got != expected {
		t.Errorf("ResolvePath() = %q, want %q", got, expected)
	}
}

func TestResolvePathEmpty(t *testing.T) {
	_, err := ResolvePath("any/config.lua", "")
	if err == nil {
		t.Fatalf("ResolvePath() should error on empty secretsFile")
	}
}
