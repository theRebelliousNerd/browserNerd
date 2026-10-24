package config

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

type DisplayBackendStatus struct {
	Name                string   `json:"name"`
	Supported           bool     `json:"supported"`
	Available           bool     `json:"available"`
	MissingDependencies []string `json:"missing_dependencies,omitempty"`
}

// DisplayBackend reports capability before Chrome starts, without creating a
// process, connecting to the user's display, or exposing environment values.
func DisplayBackend() DisplayBackendStatus {
	status := DisplayBackendStatus{Name: "xvfb-openbox", Supported: runtime.GOOS == "linux"}
	if !status.Supported {
		return status
	}
	for _, name := range []string{"Xvfb", "openbox"} {
		if _, err := exec.LookPath(name); err != nil {
			status.MissingDependencies = append(status.MissingDependencies, name)
		}
	}
	status.Available = len(status.MissingDependencies) == 0
	return status
}

func (b BrowserConfig) ValidateDisplayIsolation() error {
	if !b.IsolatedDisplay {
		return nil
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("browser.isolated_display requires the Linux Xvfb backend; unsupported platform %s", runtime.GOOS)
	}
	if b.IsHeadless() {
		return fmt.Errorf("browser.isolated_display requires browser.headless: false")
	}
	if b.DebuggerURL != "" {
		return fmt.Errorf("browser.isolated_display cannot attach to debugger_url; it requires a managed browser")
	}
	for _, arg := range b.Launch[min(1, len(b.Launch)):] {
		name, _, _ := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		switch name {
		case "user-data-dir", "display", "headless":
			return fmt.Errorf("browser.isolated_display owns the profile, display and headed mode; launch flag --%s conflicts", name)
		}
	}
	return nil
}
