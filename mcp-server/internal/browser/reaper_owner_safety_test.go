package browser

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"browsernerd-mcp-server/internal/config"
)

func TestReapOrphansUnqualifiedOwnerCannotAuthorizeKill(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rod", "user-data", "unqualified")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ProfileOwnerFile), []byte(strconv.Itoa(9999999)), 0o600); err != nil {
		t.Fatal(err)
	}
	q := &mockQuerier{procs: []ProcessInfo{{PID: 123, CommandLine: "chrome --user-data-dir=" + dir}}}
	m := NewSessionManager(config.BrowserConfig{}, nil)
	m.SetProcessQuerier(q)
	count, err := m.ReapOrphans(context.Background())
	if err != nil || count != 0 || len(q.getKilledPIDs()) != 0 {
		t.Fatalf("unqualified PID authorized cleanup: count=%d killed=%v err=%v", count, q.getKilledPIDs(), err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("unqualified profile was deleted: %v", err)
	}
}

func TestReapOrphansForeignProcessScopeCannotAuthorizeKill(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rod", "user-data", "foreign-scope")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(profileOwnership{PID: 1, ProcessScope: "foreign:pid-namespace"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ProfileOwnerFile), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	q := &mockQuerier{procs: []ProcessInfo{{PID: 123, CommandLine: "chrome --user-data-dir=" + dir}}}
	m := NewSessionManager(config.BrowserConfig{}, nil)
	m.SetProcessQuerier(q)
	count, err := m.ReapOrphans(context.Background())
	if err != nil || count != 0 || len(q.getKilledPIDs()) != 0 {
		t.Fatalf("foreign-scope PID authorized cleanup: count=%d killed=%v err=%v", count, q.getKilledPIDs(), err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("foreign-scope profile was deleted: %v", err)
	}
}

func TestProfileOwnerRequiresQualifiedMetadata(t *testing.T) {
	for _, raw := range []string{"1", `{}`, `{"pid":1}`, `{"pid":0,"process_scope":"linux:pid:[1]"}`, `{"pid":1,"process_scope":"foreign"}`, `{"pid":`} {
		t.Run(raw, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, ProfileOwnerFile), []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if owner := profileOwner(dir); owner != 0 {
				t.Fatalf("unqualified metadata returned owner PID %d", owner)
			}
		})
	}
}

func TestProfileOwnerMarkIsOpaqueToUnqualifiedPIDReaders(t *testing.T) {
	dir := t.TempDir()
	if err := markProfileOwner(dir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ProfileOwnerFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := strconv.Atoi(string(raw)); err == nil {
		t.Fatal("unqualified PID reader can mistake namespaced owner for a local process")
	}
	if profileOwner(dir) != os.Getpid() {
		t.Fatal("qualified owner cannot recognize its own profile")
	}
}
