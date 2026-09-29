package browser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-rod/rod/lib/launcher"

	"browsernerd-mcp-server/internal/config"
)

type mockQuerier struct {
	mu           sync.Mutex
	procs        []ProcessInfo
	queryErr     error
	alivePIDs    map[int]bool
	killedPIDs   []int
	killTreeErr  error
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

func TestReapOrphans_ReapsOnlyDeadParentTrees(t *testing.T) {
	tempBase := t.TempDir()

	orphanDir1 := filepath.Join(tempBase, "rod", "user-data", "orphan_tree_1")
	if err := os.MkdirAll(orphanDir1, 0o700); err != nil {
		t.Fatal(err)
	}

	activeDir := filepath.Join(tempBase, "rod", "user-data", "active_tree_2")
	if err := os.MkdirAll(activeDir, 0o700); err != nil {
		t.Fatal(err)
	}

	orphanDir3 := filepath.Join(tempBase, "rod", "user-data", "orphan_tree_3")
	if err := os.MkdirAll(orphanDir3, 0o700); err != nil {
		t.Fatal(err)
	}

	q := &mockQuerier{
		alivePIDs: map[int]bool{
			// Tree 1: Orphaned (launcher PPID 9999 is dead)
			100: true, // Root Chrome
			101: true, // Child utility
			102: true, // Child renderer
			// Tree 2: Active BrowserNERD instance (launcher PPID 8888 is ALIVE)
			8888: true, // Live server process!
			200:  true, // Root Chrome
			201:  true, // Child renderer
			// Tree 3: Personal Chrome (should be ignored by user-data-dir filter)
			300: true,
			// Tree 4: Orphaned partial tree (root Chrome 400 is dead, child 401 still alive)
			401: true,
		},
		procs: []ProcessInfo{
			// Tree 1
			{PID: 100, PPID: 9999, CommandLine: `chrome.exe --user-data-dir="` + orphanDir1 + `"`},
			{PID: 101, PPID: 100, CommandLine: `chrome.exe --type=utility --user-data-dir="` + orphanDir1 + `"`},
			{PID: 102, PPID: 100, CommandLine: `chrome.exe --type=renderer --user-data-dir="` + orphanDir1 + `"`},
			// Tree 2 (live parent 8888)
			{PID: 200, PPID: 8888, CommandLine: `chrome.exe --user-data-dir="` + activeDir + `"`},
			{PID: 201, PPID: 200, CommandLine: `chrome.exe --type=renderer --user-data-dir="` + activeDir + `"`},
			// Tree 3 (personal Chrome)
			{PID: 300, PPID: 1, CommandLine: `chrome.exe --user-data-dir="C:\Users\test\AppData\Local\Google\Chrome\User Data"`},
			// Tree 4 (orphaned child whose root Chrome 400 is dead)
			{PID: 401, PPID: 400, CommandLine: `chrome.exe --type=utility --user-data-dir="` + orphanDir3 + `"`},
		},
	}

	manager := NewSessionManager(config.BrowserConfig{}, nil)
	manager.SetProcessQuerier(q)

	reaped, err := manager.ReapOrphans(context.Background())
	if err != nil {
		t.Fatalf("ReapOrphans failed: %v", err)
	}

	// Exact reaped count MUST be 2 (Tree 1 and Tree 4)
	if reaped != 2 {
		t.Fatalf("expected exactly 2 reaped trees, got %d", reaped)
	}
	if manager.ReapedOrphans() != 2 {
		t.Fatalf("expected manager.ReapedOrphans() == 2, got %d", manager.ReapedOrphans())
	}

	killed := q.getKilledPIDs()
	killedMap := make(map[int]bool)
	for _, p := range killed {
		killedMap[p] = true
	}

	// Tree 1 processes must be killed
	if !killedMap[100] || !killedMap[101] || !killedMap[102] {
		t.Fatalf("expected Tree 1 processes (100, 101, 102) to be killed, killed: %v", killed)
	}
	// Tree 4 process must be killed
	if !killedMap[401] {
		t.Fatalf("expected Tree 4 process 401 to be killed, killed: %v", killed)
	}

	// Tree 2 processes (live parent 8888) must NEVER be killed
	if killedMap[200] || killedMap[201] {
		t.Fatalf("Tree 2 with live parent was killed! killed: %v", killed)
	}
	// Tree 3 (personal Chrome) must NEVER be killed
	if killedMap[300] {
		t.Fatalf("Tree 3 (personal Chrome) was killed! killed: %v", killed)
	}

	// Temp profiles for Tree 1 and 4 must be deleted from disk
	if _, err := os.Stat(orphanDir1); !os.IsNotExist(err) {
		t.Fatalf("orphanDir1 was not removed from disk: %v", err)
	}
	if _, err := os.Stat(orphanDir3); !os.IsNotExist(err) {
		t.Fatalf("orphanDir3 was not removed from disk: %v", err)
	}

	// Active profile for Tree 2 must STILL EXIST on disk
	if _, err := os.Stat(activeDir); os.IsNotExist(err) {
		t.Fatalf("activeDir was incorrectly removed from disk!")
	}

	// Mutation control: If launcher PPID 9999 is set to ALIVE, it must NOT be reaped
	q2 := &mockQuerier{
		alivePIDs: map[int]bool{
			9999: true, // Now alive!
			100:  true,
		},
		procs: []ProcessInfo{
			{PID: 100, PPID: 9999, CommandLine: `chrome.exe --user-data-dir="` + orphanDir1 + `"`},
		},
	}
	manager2 := NewSessionManager(config.BrowserConfig{}, nil)
	manager2.SetProcessQuerier(q2)
	reaped2, err2 := manager2.ReapOrphans(context.Background())
	if err2 != nil || reaped2 != 0 {
		t.Fatalf("mutation control: when parent 9999 is alive, reaped count must be 0, got %d (err: %v)", reaped2, err2)
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
