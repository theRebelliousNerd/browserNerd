package config

import (
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDisplayIsolationConfiguration(t *testing.T) {
	var parsed struct {
		Browser BrowserConfig `yaml:"browser"`
	}
	if err := yaml.Unmarshal([]byte("browser:\n  headless: false\n  isolated_display: true\n"), &parsed); err != nil {
		t.Fatal(err)
	}
	if !parsed.Browser.IsolatedDisplay || parsed.Browser.IsHeadless() {
		t.Fatalf("headed isolation configuration lost: %+v", parsed.Browser)
	}
	if err := parsed.Browser.ValidateDisplayIsolation(); (err == nil) != (runtime.GOOS == "linux") {
		t.Fatalf("platform validation: %v", err)
	}
	if err := validateWorkspaceAuthority([]byte("browser:\n  isolated_display: true\n"), t.TempDir(), false); err == nil {
		t.Fatal("untrusted workspace selected process launch policy")
	}
}

func TestDisplayIsolationRejectsConflictingModes(t *testing.T) {
	headed := false
	for _, cfg := range []BrowserConfig{
		{IsolatedDisplay: true},
		{IsolatedDisplay: true, Headless: &headed, DebuggerURL: "ws://localhost:9222"},
		{IsolatedDisplay: true, Headless: &headed, Launch: []string{"chrome", "--user-data-dir=/tmp/personal-profile"}},
		{IsolatedDisplay: true, Headless: &headed, Launch: []string{"chrome", "--display=:0"}},
		{IsolatedDisplay: true, Headless: &headed, Launch: []string{"chrome", "--headless=new"}},
	} {
		if err := cfg.ValidateDisplayIsolation(); err == nil || !strings.Contains(err.Error(), "isolated_display") {
			t.Fatalf("conflicting isolation mode was accepted: %+v: %v", cfg, err)
		}
	}
}
