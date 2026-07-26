package browser

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
)

func TestLiveSharedTabsIsolatedTabAndMultipleBrowsers(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}
	chrome := liveChromeBinary()
	if chrome == "" {
		t.Skip("Chrome binary not found for multi-browser live test")
	}
	shared := true
	cfg := config.BrowserConfig{
		Launch:          []string{chrome},
		Headless:        boolPtr(true),
		MultiTabDefault: &shared,
		MaxTabs:         6,
		MaxBrowsers:     2,
		SessionStore:    filepath.Join(t.TempDir(), "sessions.json"),
	}
	manager := NewSessionManager(cfg, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	if err := manager.Start(ctx); err != nil {
		t.Skipf("Chrome launch unavailable: %v", err)
	}
	defer manager.Shutdown(context.Background())

	first, err := manager.CreateSession(ctx, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.CreateSession(ctx, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	if first.BrowserID == "" || first.BrowserID != second.BrowserID {
		t.Fatalf("default sessions were not shared tabs: first=%+v second=%+v", first, second)
	}
	if first.Isolated || second.Isolated {
		t.Fatalf("default sessions should use the shared context: first=%+v second=%+v", first, second)
	}

	isolated, err := manager.CreateTab(ctx, first.BrowserID, "about:blank", true)
	if err != nil {
		t.Fatal(err)
	}
	if !isolated.Isolated || isolated.BrowserID != first.BrowserID {
		t.Fatalf("isolated tab metadata is incorrect: %+v", isolated)
	}

	additional, err := manager.LaunchAdditional(ctx)
	if err != nil {
		t.Fatal(err)
	}
	otherTab, err := manager.CreateTab(ctx, additional.ID, "about:blank", false)
	if err != nil {
		t.Fatal(err)
	}
	if otherTab.BrowserID != additional.ID || otherTab.BrowserID == first.BrowserID {
		t.Fatalf("tab was not assigned to the second browser: %+v", otherTab)
	}

	browsers := manager.ListBrowsers()
	if len(browsers) != 2 {
		t.Fatalf("expected two browser instances, got %+v", browsers)
	}
	if err := manager.FocusSession(second.ID); err != nil {
		t.Fatalf("focus shared tab: %v", err)
	}
	if closed, err := manager.CloseSession(second.ID); err != nil || !closed {
		t.Fatalf("close shared tab: closed=%v err=%v", closed, err)
	}
	if closed, err := manager.CloseBrowser(additional.ID); err != nil || !closed {
		t.Fatalf("close additional browser: closed=%v err=%v", closed, err)
	}
	if remaining := manager.ListBrowsers(); len(remaining) != 1 || !remaining[0].Default {
		t.Fatalf("default browser was not retained: %+v", remaining)
	}
}

func liveChromeBinary() string {
	for _, key := range []string{"BROWSERNERD_CHROME_BIN", "CHROME_PATH"} {
		if candidate := os.Getenv(key); isExecutableFile(candidate) {
			return candidate
		}
	}
	candidates := []string{
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
	}
	if runtime.GOOS == "windows" {
		candidates = append(candidates,
			filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe"),
		)
		if roaming := os.Getenv("APPDATA"); roaming != "" {
			matches, _ := filepath.Glob(filepath.Join(roaming, "rod", "browser", "chromium-*", "chrome.exe"))
			candidates = append(candidates, matches...)
		}
	}
	for _, candidate := range candidates {
		if isExecutableFile(candidate) {
			return candidate
		}
	}
	return ""
}

func isExecutableFile(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
