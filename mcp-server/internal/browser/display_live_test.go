package browser

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
)

func TestLiveIsolatedDisplayLifecycle(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("BROWSERNERD_BACKGROUND_HEADED") != "1" {
		t.Skip("explicit private headed display lifecycle proof")
	}
	headless := false
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	ambient := os.Getenv("DISPLAY")
	manager := NewSessionManager(config.BrowserConfig{Headless: &headless, IsolatedDisplay: true, SessionStore: filepath.Join(t.TempDir(), "sessions.json")}, nil)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Shutdown(context.Background())
	first := manager.browsers[manager.defaultBrowserID].display
	if first == nil || first.name == ambient || os.Getenv("DISPLAY") != ambient {
		t.Fatal("private display reused or changed ambient desktop routing")
	}
	additional, err := manager.LaunchAdditional(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second := manager.browsers[additional.ID].display
	if second == nil || first.name == second.name {
		t.Fatal("separate Chrome instances shared a private display")
	}
	for _, instance := range manager.ListBrowsers() {
		session, err := manager.CreateTab(ctx, instance.ID, "about:blank", false)
		if err != nil {
			t.Fatal(err)
		}
		if !session.IsolatedDisplay || !instance.IsolatedDisplay {
			t.Fatal("inventory lost display isolation")
		}
	}
	if closed, err := manager.CloseBrowser(additional.ID); !closed || err != nil {
		t.Fatalf("close additional: %v %v", closed, err)
	}
	assertDisplayGone(t, second)
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertDisplayGone(t, first)
	t.Logf("owned displays %s and %s stopped after their Chrome processes; ambient DISPLAY=%q unchanged", first.name, second.name, ambient)
}

func assertDisplayGone(t *testing.T, display *managedDisplay) {
	t.Helper()
	for _, path := range []string{display.dir, "/tmp/.X" + strings.TrimPrefix(display.name, ":") + "-lock", "/tmp/.X11-unix/X" + strings.TrimPrefix(display.name, ":")} {
		for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
			if _, err := os.Lstat(path); os.IsNotExist(err) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("private display artifact remained: %s: %v", path, err)
		}
	}
	for _, process := range []*displayProcess{display.xserver, display.wm} {
		if process.cmd.ProcessState == nil {
			t.Fatalf("display child %d was not reaped", process.cmd.Process.Pid)
		}
	}
}

func TestLiveIsolatedChromeLaunchFailureCleansDisplayWithoutFallback(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("BROWSERNERD_BACKGROUND_HEADED") != "1" {
		t.Skip("explicit private display failure proof")
	}
	before, err := filepath.Glob(filepath.Join(os.TempDir(), "browsernerd-display-*"))
	if err != nil {
		t.Fatal(err)
	}
	headless := false
	manager := NewSessionManager(config.BrowserConfig{Headless: &headless, IsolatedDisplay: true, Launch: []string{"/bin/false"}}, nil)
	launched, err := manager.launchChrome()
	if err == nil || launched != nil || !strings.Contains(err.Error(), "no desktop fallback attempted") {
		t.Fatalf("isolated launch failure did not refuse: %+v %v", launched, err)
	}
	after, globErr := filepath.Glob(filepath.Join(os.TempDir(), "browsernerd-display-*"))
	if globErr != nil || strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("failed Chrome launch leaked display: before=%v after=%v %v", before, after, globErr)
	}
}
