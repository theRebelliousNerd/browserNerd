package browser

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-rod/rod/lib/launcher"

	"browsernerd-mcp-server/internal/config"
)

type mockQuerier struct {
	mu          sync.Mutex
	procs       []ProcessInfo
	queryErr    error
	alivePIDs   map[int]bool
	killedPIDs  []int
	killTreeErr error
}

func (m *mockQuerier) QueryChromeProcesses(ctx context.Context) ([]ProcessInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	res := make([]ProcessInfo, len(m.procs))
	copy(res, m.procs)
	return res, nil
}

func (m *mockQuerier) IsProcessAlive(pid int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.alivePIDs == nil {
		return false
	}
	return m.alivePIDs[pid]
}

func (m *mockQuerier) KillProcessTree(pid int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.killedPIDs = append(m.killedPIDs, pid)
	if m.alivePIDs != nil {
		delete(m.alivePIDs, pid)
	}
	return m.killTreeErr
}

func (m *mockQuerier) getKilledPIDs() []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make([]int, len(m.killedPIDs))
	copy(res, m.killedPIDs)
	return res
}

func TestExtractUserDataDir_Formats(t *testing.T) {
	tests := []struct {
		name     string
		cmdLine  string
		expected string
	}{
		{
			name:     "quoted double path with flag eq",
			cmdLine:  `chrome.exe --type=renderer --user-data-dir="C:\Users\test\AppData\Local\Temp\rod\user-data\123" --flag2`,
			expected: `C:\Users\test\AppData\Local\Temp\rod\user-data\123`,
		},
		{
			name:     "unquoted path with flag eq",
			cmdLine:  `chrome.exe --user-data-dir=C:\Temp\rod\user-data\456 --no-first-run`,
			expected: `C:\Temp\rod\user-data\456`,
		},
		{
			name:     "quoted single path with flag eq",
			cmdLine:  `chrome.exe --user-data-dir='/tmp/rod/user-data/789' --no-sandbox`,
			expected: `/tmp/rod/user-data/789`,
		},
		{
			name:     "space separated quoted path",
			cmdLine:  `chrome.exe --user-data-dir "C:\Users\test\AppData\Local\Temp\rod\user-data\abc" --other`,
			expected: `C:\Users\test\AppData\Local\Temp\rod\user-data\abc`,
		},
		{
			name:     "space separated unquoted path",
			cmdLine:  `chrome.exe --user-data-dir /tmp/rod/user-data/def --headless`,
			expected: `/tmp/rod/user-data/def`,
		},
		{
			name:     "at end of command line without trailing flags",
			cmdLine:  `chrome.exe --user-data-dir=C:\Temp\rod\user-data\xyz`,
			expected: `C:\Temp\rod\user-data\xyz`,
		},
		{
			name:     "missing flag returns empty",
			cmdLine:  `chrome.exe --type=renderer --no-first-run`,
			expected: ``,
		},
		{
			name:     "empty cmdLine returns empty",
			cmdLine:  ``,
			expected: ``,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractUserDataDir(tt.cmdLine)
			if got != tt.expected {
				t.Fatalf("extractUserDataDir(%q) = %q, expected %q", tt.cmdLine, got, tt.expected)
			}
		})
	}

	// Mutation control: flag variation that does NOT match --user-data-dir must yield empty
	mutated := extractUserDataDir(`chrome.exe --user-data-path="C:\Temp\rod\user-data\123"`)
	if mutated != "" {
		t.Fatalf("mutation control failed: expected empty for non-matching flag, got %q", mutated)
	}
}

func TestIsBrowserNERDUserDataDir_Boundaries(t *testing.T) {
	// Positive matches: specific profile directories under rod/user-data or browsernerd/user-data
	positivePaths := []string{
		filepath.Join(os.TempDir(), "rod", "user-data", "abcdef123456"),
		`C:\Users\test\AppData\Local\Temp\rod\user-data\d8502ed20379c079`,
		`/tmp/rod/user-data/d8502ed20379c079`,
		`C:\Users\test\AppData\Local\Temp\browsernerd\user-data\session1`,
		`/var/tmp/browsernerd/user-data/session2`,
		filepath.Join(launcher.DefaultUserDataDirPrefix, "subprofile"),
	}

	for _, p := range positivePaths {
		if !isBrowserNERDUserDataDir(p) {
			t.Fatalf("expected isBrowserNERDUserDataDir(%q) == true, got false", p)
		}
	}

	// Negative refusals: personal Chrome profiles, root paths, parent container itself
	negativePaths := []string{
		`C:\Users\test\AppData\Local\Google\Chrome\User Data`,
		`C:\Users\test\AppData\Local\Google\Chrome\User Data\Default`,
		`/home/user/.config/google-chrome/Default`,
		`C:\Program Files\Google\Chrome\Application`,
		`C:\Windows\System32`,
		`C:\`,
		`/`,
		``,
		launcher.DefaultUserDataDirPrefix, // Root container without subprofile must be protected
	}

	for _, p := range negativePaths {
		if isBrowserNERDUserDataDir(p) {
			t.Fatalf("expected isBrowserNERDUserDataDir(%q) == false (refused), got true", p)
		}
	}
}

func TestRemoveUserDataDir_SafetyRefusal(t *testing.T) {
	// Safety refusal on forbidden directory
	err := removeUserDataDir(`C:\Users\tester\AppData\Local\Google\Chrome\User Data`)
	if err == nil || !strings.Contains(err.Error(), "safety refusal") {
		t.Fatalf("expected safety refusal for personal Chrome dir, got: %v", err)
	}

	// Empty path is a no-op
	if err := removeUserDataDir(""); err != nil {
		t.Fatalf("expected nil for empty dir, got: %v", err)
	}

	// Permitted deletion on actual temp profile dir
	tempBase := t.TempDir()
	profileDir := filepath.Join(tempBase, "rod", "user-data", "test_profile_1")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatalf("failed to create test profile dir: %v", err)
	}
	testFile := filepath.Join(profileDir, "Cookies")
	if err := os.WriteFile(testFile, []byte("data"), 0o600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := removeUserDataDir(profileDir); err != nil {
		t.Fatalf("expected successful deletion, got: %v", err)
	}
	if _, err := os.Stat(profileDir); !os.IsNotExist(err) {
		t.Fatalf("expected directory %q to be deleted, stat err: %v", profileDir, err)
	}
}

func TestReapOrphans_ReapsOnlyTreesWhoseOwnerDied(t *testing.T) {
	tempBase := t.TempDir()
	profile := func(name string, owner int) string {
		t.Helper()
		dir := filepath.Join(tempBase, "rod", "user-data", name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if owner > 0 {
			scope, err := processScope()
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(profileOwnership{PID: owner, ProcessScope: scope})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, ProfileOwnerFile), raw, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	orphanDir := profile("orphan", 9999)   // owner 9999 is dead
	activeDir := profile("active", 8888)   // owner 8888 is alive
	partialDir := profile("partial", 7777) // owner dead, root gone, child alive
	ownDir := profile("own", os.Getpid())  // launched by this process
	unmarkedDir := profile("other-rod", 0) // another Rod program's profile

	q := &mockQuerier{
		// The parent of every orphan is a live subreaper ("systemd --user"),
		// which must not read as a live server.
		alivePIDs: map[int]bool{8888: true, 100: true, 101: true, 200: true, 300: true, 401: true, 500: true, 600: true},
		procs: []ProcessInfo{
			{PID: 100, CommandLine: `chrome --user-data-dir=` + orphanDir},
			{PID: 101, CommandLine: `chrome --type=renderer --user-data-dir=` + orphanDir},
			{PID: 200, CommandLine: `chrome --user-data-dir=` + activeDir},
			{PID: 300, CommandLine: `chrome --user-data-dir="/home/test/.config/google-chrome"`},
			{PID: 401, CommandLine: `chrome --type=utility --user-data-dir=` + partialDir},
			{PID: 500, CommandLine: `chrome --user-data-dir=` + ownDir},
			{PID: 600, CommandLine: `chrome --user-data-dir=` + unmarkedDir},
		},
	}

	manager := NewSessionManager(config.BrowserConfig{}, nil)
	manager.SetProcessQuerier(q)
	reaped, err := manager.ReapOrphans(context.Background())
	if err != nil {
		t.Fatalf("ReapOrphans failed: %v", err)
	}
	if reaped != 2 || manager.ReapedOrphans() != 2 {
		t.Fatalf("expected the orphan and partial trees reaped, got %d (total %d)", reaped, manager.ReapedOrphans())
	}

	killed := map[int]bool{}
	for _, pid := range q.getKilledPIDs() {
		killed[pid] = true
	}
	for _, pid := range []int{100, 101, 401} {
		if !killed[pid] {
			t.Errorf("orphaned process %d was not killed (killed %v)", pid, q.getKilledPIDs())
		}
	}
	for _, pid := range []int{200, 300, 500, 600} {
		if killed[pid] {
			t.Errorf("process %d with a live or unknown owner was killed", pid)
		}
	}
	for _, dir := range []string{orphanDir, partialDir} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("orphaned profile %s was not removed: %v", dir, err)
		}
	}
	for _, dir := range []string{activeDir, ownDir, unmarkedDir} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("profile %s was removed: %v", dir, err)
		}
	}
}

func TestMarkProfileOwnerRecordsThisProcess(t *testing.T) {
	dir := t.TempDir()
	if got := profileOwner(dir); got != 0 {
		t.Fatalf("unmarked profile has owner %d", got)
	}
	if err := markProfileOwner(dir); err != nil {
		t.Fatal(err)
	}
	if got := profileOwner(dir); got != os.Getpid() {
		t.Fatalf("owner %d, want %d", got, os.Getpid())
	}
}

func TestShutdown_KillsProcessTreeAndDeletesTempDir(t *testing.T) {
	tempBase := t.TempDir()
	sessionDir := filepath.Join(tempBase, "rod", "user-data", "sess_cleanup")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}

	q := &mockQuerier{
		alivePIDs: map[int]bool{500: true},
	}

	manager := NewSessionManager(config.BrowserConfig{}, nil)
	manager.SetProcessQuerier(q)

	manager.mu.Lock()
	manager.browsers["test-b"] = &browserRecord{
		meta:        BrowserInstance{ID: "test-b", Status: "connected"},
		pid:         500,
		userDataDir: sessionDir,
		tempDir:     true,
	}
	manager.mu.Unlock()

	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	killed := q.getKilledPIDs()
	if len(killed) != 1 || killed[0] != 500 {
		t.Fatalf("expected PID 500 to be killed, got: %v", killed)
	}

	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Fatalf("expected sessionDir to be deleted after Shutdown, stat err: %v", err)
	}
}

func TestCloseBrowser_KillsProcessTreeAndDeletesTempDir(t *testing.T) {
	tempBase := t.TempDir()
	bDir := filepath.Join(tempBase, "rod", "user-data", "close_browser_test")
	if err := os.MkdirAll(bDir, 0o700); err != nil {
		t.Fatal(err)
	}

	q := &mockQuerier{
		alivePIDs: map[int]bool{600: true},
	}

	manager := NewSessionManager(config.BrowserConfig{}, nil)
	manager.SetProcessQuerier(q)

	manager.mu.Lock()
	manager.browsers["browser-kill"] = &browserRecord{
		meta:        BrowserInstance{ID: "browser-kill", Status: "connected"},
		pid:         600,
		userDataDir: bDir,
		tempDir:     true,
	}
	manager.mu.Unlock()

	closed, err := manager.CloseBrowser("browser-kill")
	if err != nil {
		t.Fatalf("CloseBrowser failed: %v", err)
	}
	if !closed {
		t.Fatalf("expected closed == true")
	}

	killed := q.getKilledPIDs()
	if len(killed) != 1 || killed[0] != 600 {
		t.Fatalf("expected PID 600 to be killed, got: %v", killed)
	}

	if _, err := os.Stat(bDir); !os.IsNotExist(err) {
		t.Fatalf("expected bDir to be deleted after CloseBrowser, stat err: %v", err)
	}
}
