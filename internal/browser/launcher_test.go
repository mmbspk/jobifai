package browser

import (
	"testing"

	"github.com/go-rod/rod/lib/launcher/flags"
)

func TestNewLauncherHonorsBrowserEnvironment(t *testing.T) {
	// A nonexistent explicit path must remain explicit: silently switching to
	// Rod's old binary would hide a deployment configuration error.
	t.Setenv("ROD_BROWSER_BIN", "/configured/current-chrome")
	if got := NewLauncher().Get(flags.Bin); got != "/configured/current-chrome" {
		t.Fatalf("got browser %q", got)
	}
}

func TestSelectBrowserBinary(t *testing.T) {
	for _, tc := range []struct {
		name, configured, rodDefault, installed, want string
	}{
		{"configured wins", "/configured", "/rod", "/installed", "/configured"},
		{"rod setting preserved", "", "/rod", "/installed", "/rod"},
		{"installed before bundled", "", "", "/installed", "/installed"},
		{"bundled fallback", "", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			looked := false
			got := selectBrowserBinary(tc.configured, tc.rodDefault, func() (string, bool) {
				looked = true
				return tc.installed, tc.installed != ""
			})
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if looked && (tc.configured != "" || tc.rodDefault != "") {
				t.Fatal("must not discover another browser when explicitly configured")
			}
		})
	}
}

func TestSeekBrowserUpgradeURL(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"https://au.seek.com/browser-upgrade?continue=https%3A%2F%2Fau.seek.com%2F", true},
		{"https://www.seek.com.au/browser-upgrade/", true},
		{"https://AU.SEEK.COM/browser-upgrade", true},
		{"https://au.seek.com/jobs", false},
		{"https://au.seek.com/jobs?continue=/browser-upgrade", false},
		{"https://seek.com.example.org/browser-upgrade", false},
		{"https://notseek.com/browser-upgrade", false},
		{"about:blank", false},
		{"%", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			if got := isSeekBrowserUpgrade(tc.url); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
