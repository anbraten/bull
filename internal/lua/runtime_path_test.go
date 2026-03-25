package lua

import "testing"

func TestResolveLocalPath(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		configDir string
		want      string
	}{
		{name: "empty", path: "", configDir: "/cfg", want: ""},
		{name: "relative", path: "keys/id_ed25519", configDir: "/cfg", want: "/cfg/keys/id_ed25519"},
		{name: "relative with dot", path: "./keys/id_ed25519", configDir: "/cfg", want: "/cfg/keys/id_ed25519"},
		{name: "parent segments", path: "../.ssh/id", configDir: "/cfg/sub", want: "/cfg/.ssh/id"},
		{name: "absolute", path: "/tmp/key", configDir: "/cfg", want: "/tmp/key"},
		{name: "home path", path: "~/.ssh/id_ed25519", configDir: "/cfg", want: "~/.ssh/id_ed25519"},
		{name: "no config dir", path: "keys/id", configDir: "", want: "keys/id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveLocalPath(tt.path, tt.configDir)
			if got != tt.want {
				t.Fatalf("resolveLocalPath(%q, %q) = %q, want %q", tt.path, tt.configDir, got, tt.want)
			}
		})
	}
}
