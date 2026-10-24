package mcp

import (
	"context"
	"strings"
	"testing"

	"browsernerd-mcp-server/internal/config"
)

func TestDoctorReportsRequestedDisplayIsolationBeforeLaunch(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	cfg := config.DefaultConfig()
	headless := false
	cfg.Browser.Headless, cfg.Browser.IsolatedDisplay = &headless, true
	report := DoctorReport(context.Background(), DoctorDeps{Config: cfg})
	resolved := report["config"].(map[string]interface{})
	backend, ok := resolved["display_backend"].(config.DisplayBackendStatus)
	if resolved["isolated_display"] != true || resolved["headless"] != false || !ok || backend.Available || backend.Name != "xvfb-openbox" {
		t.Fatalf("doctor cannot preflight headed isolation: %+v", resolved)
	}
	found := false
	for _, problem := range report["problems"].([]string) {
		if strings.Contains(problem, "isolated headed display backend is unavailable") {
			found = true
		}
	}
	if !found {
		t.Fatal("requested unavailable isolation was not reported as a problem")
	}
}
