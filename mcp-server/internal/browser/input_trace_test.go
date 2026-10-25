package browser

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
	"github.com/go-rod/rod"
)

func TestInputTracePrivacyProjectionAndUnknownFinal(t *testing.T) {
	options := InputTraceOptions{DurationMS: 100, MaxEvents: 32, MaxBytes: 32768}
	trace := &InputTrace{Options: options, ready: make(chan struct{})}
	payload := `{"active":true,"reason":"","target_ref":"n0","nodes":[{"ref":"n0","kind":"button","text":"PrivateTextCanary"}],"events":[{"event_id":1,"kind":"click","target_ref":"n0","path":["n0"],"trusted":true,"phases":[{"current_target":"n0","phase":2,"listener_capture":false}],"value":"PrivateValueCanary","final":{"confirmed":false,"default_prevented":null,"propagation":"not_observable_after_dispatch"}}],"event_count":1,"phase_count":1,"final_confirmed":0}`
	if !trace.accept(payload, false) {
		t.Fatal("fixed event metadata rejected")
	}
	snapshot, _, _ := trace.Snapshot()
	raw, _ := json.Marshal(snapshot)
	if strings.Contains(string(raw), "Canary") || snapshot.Events[0].Final.DefaultPrevented != nil || snapshot.Events[0].Final.Confirmed {
		t.Fatalf("private or invented final value: %s", raw)
	}
	for _, bad := range []string{
		strings.Replace(payload, `"kind":"click"`, `"kind":"PrivateKindCanary"`, 1),
		strings.Replace(payload, `"ref":"n0"`, `"ref":"PrivateAttributeCanary"`, 1),
		strings.Replace(payload, `"kind":"button"`, `"kind":"PrivateTagCanary"`, 1),
		strings.Replace(payload, `"confirmed":false`, `"confirmed":true`, 1),
		strings.Replace(payload, `"kind":"click"`, `"kind":"keydown","key_code":"KeyA"`, 1),
		strings.Repeat("x", 32769),
	} {
		if trace.accept(bad, false) {
			t.Fatal("unapproved trace string/metadata accepted")
		}
	}
	snapshot, _, _ = trace.Snapshot()
	if snapshot.InvalidUpdates != 6 {
		t.Fatalf("invalid updates disappeared: %d", snapshot.InvalidUpdates)
	}
	trace.mu.Lock()
	trace.stopping = true
	trace.mu.Unlock()
	if trace.accept(payload, false) {
		t.Fatal("stale queued update can overwrite final stop snapshot")
	}
	if !trace.accept(payload, true) {
		t.Fatal("authoritative final snapshot refused")
	}
}

func TestInputTraceExactSessionAndRetention(t *testing.T) {
	manager := NewSessionManager(config.BrowserConfig{}, nil)
	trace := &InputTrace{ID: "own-id", closed: true, expires: time.Now().Add(-time.Second)}
	manager.sessions["own"] = &sessionRecord{page: &rod.Page{}, inputTrace: trace}
	manager.sessions["foreign"] = &sessionRecord{page: &rod.Page{}}
	for _, item := range [][2]string{{"own", "own-id"}, {"foreign", "own-id"}, {"own", "foreign-id"}, {"absent", "own-id"}} {
		if _, err := manager.InputTrace(item[0], item[1]); err == nil {
			t.Fatalf("expired/foreign trace admitted: %v", item)
		}
	}
	trace.expires = time.Now().Add(time.Minute)
	if got, err := manager.InputTrace("own", "own-id"); err != nil || got != trace {
		t.Fatalf("retained terminal receipt unavailable: %v", err)
	}
}

func TestLiveInputTraceTransportDisconnectReleasesHandles(t *testing.T) {
	if os.Getenv("BROWSERNERD_BACKGROUND_HEADED") != "1" {
		t.Skip("opt-in isolated headed transport proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	headless := false
	manager := NewSessionManager(config.BrowserConfig{Headless: &headless, IsolatedDisplay: true, SessionStore: filepath.Join(t.TempDir(), "sessions.json")}, nil)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Shutdown(context.Background())
	session, err := manager.CreateSession(ctx, "data:text/html,<button id='target'>Native target</button>")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := manager.Page(session.ID)
	root, err := page.Element("#target")
	if err != nil {
		t.Fatal(err)
	}
	trace, err := manager.ArmInputTrace(ctx, session.ID, root, InputTraceOptions{DurationMS: 30000, MaxEvents: 32, MaxBytes: 32768})
	if err != nil {
		t.Fatal(err)
	}
	if err := page.Browser().Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-trace.done:
	case <-time.After(3 * time.Second):
		t.Fatal("closed owned transport retained active trace")
	}
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if !trace.closed || trace.object != nil || trace.page != nil || trace.cleanupConfirmed {
		t.Fatalf("disconnect cleanup fabricated or retained handles: closed=%t object=%v page=%v confirmed=%t", trace.closed, trace.object, trace.page, trace.cleanupConfirmed)
	}
	if trace.snapshot.Reason != "disconnected" {
		t.Fatalf("disconnect hidden: %s", trace.snapshot.Reason)
	}
	t.Log("owned transport disconnect released local page/object handles; remote cleanup remains explicitly unconfirmed")
}

func TestLiveInputTraceSessionCloseDuringArm(t *testing.T) {
	if os.Getenv("BROWSERNERD_BACKGROUND_HEADED") != "1" {
		t.Skip("opt-in isolated headed arm/close proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	headless := false
	manager := NewSessionManager(config.BrowserConfig{Headless: &headless, IsolatedDisplay: true, SessionStore: filepath.Join(t.TempDir(), "sessions.json")}, nil)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Shutdown(context.Background())
	session, err := manager.CreateSession(ctx, "data:text/html,<button id='target'>Native target</button>")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := manager.Page(session.ID)
	root, err := page.Element("#target")
	if err != nil {
		t.Fatal(err)
	}
	armed := make(chan struct{})
	go func() {
		_, _ = manager.ArmInputTrace(ctx, session.ID, root, InputTraceOptions{DurationMS: 30000, MaxEvents: 32, MaxBytes: 32768})
		close(armed)
	}()
	var trace *InputTrace
	for trace == nil {
		manager.mu.RLock()
		if record := manager.sessions[session.ID]; record != nil {
			trace = record.inputTrace
		}
		manager.mu.RUnlock()
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := manager.CloseSession(session.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-armed:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-trace.done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if trace.object != nil || trace.page != nil {
		t.Fatal("arm/close race retained owned runtime handles")
	}
	t.Log("session close during arm completed bounded cleanup without retaining newly returned runtime handles")
}
