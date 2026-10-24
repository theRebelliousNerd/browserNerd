package browser

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/cdp"
)

type connectionProbeClient struct {
	mu    sync.Mutex
	err   error
	block <-chan struct{}
	pings int
}

func (*connectionProbeClient) Event() <-chan *cdp.Event { return nil }
func (c *connectionProbeClient) Call(_ context.Context, _, method string, _ interface{}) ([]byte, error) {
	if method != "Browser.getVersion" {
		return []byte(`{}`), nil
	}
	c.mu.Lock()
	c.pings++
	c.mu.Unlock()
	if c.block != nil {
		<-c.block
	}
	if c.err != nil {
		return nil, c.err
	}
	return []byte(`{"protocolVersion":"1.3","product":"Generic test browser"}`), nil
}

func probeTestBrowser(t *testing.T, client *connectionProbeClient) *rod.Browser {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	b := rod.New().Context(ctx).NoDefaultDevice().Client(client)
	if err := b.Connect(); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestConnectionHealthRejectsStaleBrowserPointer(t *testing.T) {
	client := &connectionProbeClient{err: net.ErrClosed}
	b := probeTestBrowser(t, client)
	m := NewSessionManager(config.BrowserConfig{}, nil)
	m.browser = b
	m.defaultBrowserID = "owned-default"
	m.browsers["owned-default"] = &browserRecord{meta: BrowserInstance{ID: "owned-default", Default: true, Managed: true}, browser: b}
	if m.IsConnected() {
		t.Fatal("closed CDP transport reported connected from pointer metadata")
	}
	client.mu.Lock()
	pings := client.pings
	client.mu.Unlock()
	if pings != 1 {
		t.Fatalf("expected actual Browser.getVersion probe, got %d", pings)
	}
}

func exitedProbePID(t *testing.T) int {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^NoConnectionProbeTest$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func ownedClosedManager(t *testing.T, client *connectionProbeClient) (*SessionManager, *mockQuerier) {
	t.Helper()
	b := probeTestBrowser(t, client)
	profile := filepath.Join(t.TempDir(), "rod", "user-data", "owned")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := markProfileOwner(profile); err != nil {
		t.Fatal(err)
	}
	m := NewSessionManager(config.BrowserConfig{Launch: []string{"/bin/false"}}, nil)
	m.browser = b
	m.defaultBrowserID = "owned-default"
	m.browsers["owned-default"] = &browserRecord{meta: BrowserInstance{ID: "owned-default", Default: true, Managed: true}, browser: b, pid: exitedProbePID(t), userDataDir: profile, tempDir: true}
	q := &mockQuerier{}
	m.querier = q
	return m, q
}

func TestConnectionHealthTimeoutAndUnknownNeverClean(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		blocked bool
		reason  string
	}{
		{"timeout", nil, true, "cdp_probe_timeout"},
		{"unknown", errors.New("generic protocol failure"), false, "cdp_probe_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			release := make(chan struct{})
			if tc.blocked {
				defer close(release)
			}
			client := &connectionProbeClient{err: tc.err}
			if tc.blocked {
				client.block = release
			}
			m, q := ownedClosedManager(t, client)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			h := m.ConnectionHealth(ctx)
			if h.Connected || h.State != "unverified" || h.Reason != tc.reason {
				t.Fatalf("ambiguous probe invented dead/alive: %+v", h)
			}
			if err := m.retireClosedDefault(h); err == nil {
				t.Fatal("unverified transport authorized cleanup")
			}
			if len(m.browsers) != 1 || len(q.getKilledPIDs()) != 0 {
				t.Fatal("probe/refusal changed browser lifecycle")
			}
		})
	}
}

func TestConnectionRecoveryGuardsAndPIDReuse(t *testing.T) {
	for _, name := range []string{"attached", "open_context", "foreign_owner", "missing_owner", "live_or_reused_pid", "unknown_pid"} {
		t.Run(name, func(t *testing.T) {
			m, q := ownedClosedManager(t, &connectionProbeClient{err: net.ErrClosed})
			r := m.browsers[m.defaultBrowserID]
			switch name {
			case "attached":
				r.meta.Managed = false
			case "open_context":
				m.sessions["open"] = &sessionRecord{meta: Session{ID: "open", BrowserID: r.meta.ID}, page: &rod.Page{}}
			case "foreign_owner":
				if err := os.WriteFile(filepath.Join(r.userDataDir, ProfileOwnerFile), []byte(strconv.Itoa(os.Getpid()+1)), 0o600); err != nil {
					t.Fatal(err)
				}
			case "missing_owner":
				if err := os.Remove(filepath.Join(r.userDataDir, ProfileOwnerFile)); err != nil {
					t.Fatal(err)
				}
			case "live_or_reused_pid":
				r.pid = os.Getpid()
			case "unknown_pid":
				r.pid = 0
			}
			h := m.ConnectionHealth(context.Background())
			if err := m.retireClosedDefault(h); err == nil {
				t.Fatal("unsafe retirement accepted")
			}
			if len(m.browsers) != 1 || m.browsers[r.meta.ID] != r || len(q.getKilledPIDs()) != 0 {
				t.Fatal("refusal modified instances or signaled a PID")
			}
			if _, err := os.Stat(r.userDataDir); err != nil {
				t.Fatal("refusal deleted profile")
			}
		})
	}
}

func TestConnectionRecoveryPreservesHealthySiblingAndSendsNoSignal(t *testing.T) {
	m, q := ownedClosedManager(t, &connectionProbeClient{err: net.ErrClosed})
	healthy := probeTestBrowser(t, &connectionProbeClient{})
	m.browsers["healthy"] = &browserRecord{meta: BrowserInstance{ID: "healthy", Managed: true}, browser: healthy}
	m.sessions["healthy-tab"] = &sessionRecord{meta: Session{ID: "healthy-tab", BrowserID: "healthy"}, page: &rod.Page{}}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.defaultBrowserID != "healthy" || m.browser != healthy || len(m.browsers) != 1 || m.sessions["healthy-tab"] == nil || len(q.getKilledPIDs()) != 0 {
		t.Fatalf("recovery erased sibling/context or signaled reused PID: browsers=%v sessions=%v killed=%v", m.ListBrowsers(), m.List(), q.getKilledPIDs())
	}
}

func TestConnectionProcessExitProbeHonest(t *testing.T) {
	if exited, err := ownedProcessExited(os.Getpid()); exited || err != nil {
		t.Fatalf("current PID invented dead: %v %v", exited, err)
	}
	if exited, err := ownedProcessExited(0); exited || err == nil {
		t.Fatalf("invalid PID invented dead: %v %v", exited, err)
	}
	if exited, err := ownedProcessExited(exitedProbePID(t)); !exited || err != nil {
		t.Fatalf("waited owned process not recognized: %v %v", exited, err)
	}
}

func TestConnectionRecoveryTimeoutStartDoesNotCleanup(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	m, q := ownedClosedManager(t, &connectionProbeClient{block: release})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := m.Start(ctx); err == nil || !strings.Contains(err.Error(), "unverified") {
		t.Fatalf("ambiguous Start did not refuse: %v", err)
	}
	if len(m.browsers) != 1 || len(q.getKilledPIDs()) != 0 {
		t.Fatal("timeout caused lifecycle cleanup")
	}
}
