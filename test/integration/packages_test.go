//go:build integration

package integration

import "testing"

// jq is a small package that's present in both distros' repos but not
// preinstalled in either base image, so installing it is a real create.
func TestPackageManagers(t *testing.T) {
	NewSuite(t, debianTarget).
		Case("apt_installs_package", func(t *testing.T, h *harness) {
			cfg := h.writeConfig(`
apt("pkg", {
  host     = "h",
  packages = { "jq" },
  update   = true,
})
`)
			if out, err := h.apply(cfg, false); err != nil {
				t.Fatalf("apply failed: %v\n%s", err, out)
			}
			if _, code := h.exec("dpkg -s jq >/dev/null 2>&1"); code != 0 {
				t.Fatalf("jq not installed after apply")
			}
		}).
		Run()

	NewSuite(t, alpineTarget).
		Case("apk_installs_package", func(t *testing.T, h *harness) {
			cfg := h.writeConfig(`
apk("pkg", {
  host     = "h",
  packages = { "jq" },
  update   = true,
})
`)
			if out, err := h.apply(cfg, false); err != nil {
				t.Fatalf("apply failed: %v\n%s", err, out)
			}
			if _, code := h.exec("apk info -e jq >/dev/null 2>&1"); code != 0 {
				t.Fatalf("jq not installed after apply")
			}
		}).
		Run()
}
