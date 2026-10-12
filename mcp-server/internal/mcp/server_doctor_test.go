package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
)

type doctorMockQuerier struct {
	mu        sync.Mutex
	procs     []browser.ProcessInfo
	alivePIDs map[int]bool
}

func (m *doctorMockQuerier) QueryChromeProcesses(ctx context.Context) ([]browser.ProcessInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make([]browser.ProcessInfo, len(m.procs))
	copy(res, m.procs)
	return res, nil
}

func (m *doctorMockQuerier) IsProcessAlive(pid int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.alivePIDs[pid]
}

func (m *doctorMockQuerier) KillProcessTree(pid int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.alivePIDs, pid)
	return nil
}

func TestDoctorReport_ReportsReapedOrphans_Direct(t *testing.T) {
	ctx := context.Background()
	deps := DoctorDeps{
		Config:        config.DefaultConfig(),
		ReapedOrphans: 9,
	}

	report := DoctorReport(ctx, deps)
	reapedVal, ok := report["reaped_orphans"]
	if !ok {
		t.Fatalf("expected report to contain 'reaped_orphans' key, got keys: %v", report)
	}
	reapedInt, ok := reapedVal.(int)
	if !ok || reapedInt != 9 {
		t.Fatalf("expected reaped_orphans == 9, got %v (type %T)", reapedVal, reapedVal)
	}

	// Mutation control: distinct count must be faithfully reported
	deps.ReapedOrphans = 0
	reportZero := DoctorReport(ctx, deps)
	if reportZero["reaped_orphans"] != 0 {
		t.Fatalf("expected reaped_orphans == 0, got %v", reportZero["reaped_orphans"])
	}
}

func TestDoctorReport_ReportsReapedOrphans_FromSessions(t *testing.T) {
	ctx := context.Background()
	tempBase := t.TempDir()

	orphanDir1 := orphanProfile(t, tempBase, "orphan_doc_1", 991)
	orphanDir2 := orphanProfile(t, tempBase, "orphan_doc_2", 992)
	orphanDir3 := orphanProfile(t, tempBase, "orphan_doc_3", 993)

	q := &doctorMockQuerier{
		alivePIDs: map[int]bool{10: true, 20: true, 30: true}, // Owners 991, 992, 993 are dead
		procs: []browser.ProcessInfo{
			{PID: 10, CommandLine: `chrome.exe --user-data-dir="` + orphanDir1 + `"`},
			{PID: 20, CommandLine: `chrome.exe --user-data-dir="` + orphanDir2 + `"`},
			{PID: 30, CommandLine: `chrome.exe --user-data-dir="` + orphanDir3 + `"`},
		},
	}

	sessions := browser.NewSessionManager(config.BrowserConfig{}, nil)
	sessions.SetProcessQuerier(q)

	reaped, err := sessions.ReapOrphans(ctx)
	if err != nil {
		t.Fatalf("ReapOrphans failed: %v", err)
	}
	if reaped != 3 {
		t.Fatalf("expected exactly 3 reaped orphans, got %d", reaped)
	}

	deps := DoctorDeps{
		Config:   config.DefaultConfig(),
		Sessions: sessions,
	}

	report := DoctorReport(ctx, deps)

	// Verify top-level report["reaped_orphans"]
	if report["reaped_orphans"] != 3 {
		t.Fatalf("expected report['reaped_orphans'] == 3, got %v", report["reaped_orphans"])
	}

	// Verify report["browser"]["reaped_orphans"]
	browserObj, ok := report["browser"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected report['browser'] map, got %v", report["browser"])
	}
	if browserObj["reaped_orphans"] != 3 {
		t.Fatalf("expected browser['reaped_orphans'] == 3, got %v", browserObj["reaped_orphans"])
	}
}

func TestBrowserObserveDoctorTool_ReportsReapedOrphans(t *testing.T) {
	ctx := context.Background()
	tempBase := t.TempDir()

	orphanDir := orphanProfile(t, tempBase, "orphan_tool_1", 999)

	q := &doctorMockQuerier{
		alivePIDs: map[int]bool{111: true}, // Owner 999 is dead
		procs: []browser.ProcessInfo{
			{PID: 111, CommandLine: `chrome.exe --user-data-dir="` + orphanDir + `"`},
		},
	}

	sessions := browser.NewSessionManager(config.BrowserConfig{}, nil)
	sessions.SetProcessQuerier(q)

	reaped, err := sessions.ReapOrphans(ctx)
	if err != nil {
		t.Fatalf("ReapOrphans failed: %v", err)
	}
	if reaped != 1 {
		t.Fatalf("expected reaped == 1, got %d", reaped)
	}

	engine, err := mangle.NewEngine(config.MangleConfig{})
	if err != nil {
		t.Fatalf("failed to create mangle engine: %v", err)
	}

	server, err := NewServer(config.DefaultConfig(), sessions, engine)
	if err != nil {
		t.Fatalf("failed to create MCP server: %v", err)
	}
	defer func() { _ = server.Close() }()

	toolResult, err := server.ExecuteTool("browser-observe", map[string]interface{}{
		"mode": "doctor",
	})
	if err != nil {
		t.Fatalf("browser-observe mode=doctor failed: %v", err)
	}

	resultBytes, err := json.Marshal(toolResult)
	if err != nil {
		t.Fatalf("failed to marshal tool result: %v", err)
	}

	var resultMap map[string]interface{}
	if err := json.Unmarshal(resultBytes, &resultMap); err != nil {
		t.Fatalf("failed to unmarshal result map: %v", err)
	}

	dataMap, ok := resultMap["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data map in tool result, got %v", resultMap)
	}

	// Verify reaped_orphans is 1
	if floatVal, ok := dataMap["reaped_orphans"].(float64); !ok || int(floatVal) != 1 {
		t.Fatalf("expected tool result reaped_orphans == 1, got %v", dataMap["reaped_orphans"])
	}

	browserMap, ok := dataMap["browser"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected browser map in data, got %v", dataMap["browser"])
	}
	if floatVal, ok := browserMap["reaped_orphans"].(float64); !ok || int(floatVal) != 1 {
		t.Fatalf("expected browser.reaped_orphans == 1, got %v", browserMap["reaped_orphans"])
	}
}

// orphanProfile creates a Rod temporary profile marked as owned by a server
// process that is no longer running.
func orphanProfile(t *testing.T, base, name string, owner int) string {
	t.Helper()
	dir := filepath.Join(base, "rod", "user-data", name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, browser.ProfileOwnerFile), []byte(strconv.Itoa(owner)), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}
