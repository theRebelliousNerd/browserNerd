package browser

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestPrivateDisplayEnvironmentDoesNotMutateAmbient(t *testing.T) {
	ambient := []string{"DISPLAY=:0", "WAYLAND_DISPLAY=wayland-0", "XAUTHORITY=/personal", "DBUS_SESSION_BUS_ADDRESS=desktop", "XDG_SESSION_TYPE=wayland", "KEEP=value"}
	before := strings.Join(ambient, "\n")
	env := privateDisplayEnvironment(ambient, ":23456")
	if strings.Join(ambient, "\n") != before {
		t.Fatal("private display changed ambient environment slice")
	}
	values := map[string]string{}
	for _, entry := range env {
		name, value, _ := strings.Cut(entry, "=")
		if _, exists := values[name]; exists {
			t.Fatalf("duplicated environment key %s", name)
		}
		values[name] = value
	}
	if values["DISPLAY"] != ":23456" || values["WAYLAND_DISPLAY"] != "" || values["XAUTHORITY"] != "" || values["DBUS_SESSION_BUS_ADDRESS"] != "" || values["XDG_SESSION_TYPE"] != "x11" || values["KEEP"] != "value" {
		t.Fatalf("desktop routing escaped private environment: %+v", values)
	}
}

func TestPrivateDisplayMissingDependenciesRefusesWithoutChangingDesktop(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux backend dependency refusal")
	}
	t.Setenv("PATH", t.TempDir())
	before := os.Getenv("DISPLAY")
	display, err := startManagedDisplay(1280, 1024)
	if err == nil || display != nil || !strings.Contains(err.Error(), "requires Xvfb") {
		t.Fatalf("missing dependency did not refuse: %+v %v", display, err)
	}
	if os.Getenv("DISPLAY") != before {
		t.Fatal("failed display setup changed desktop routing")
	}
}
