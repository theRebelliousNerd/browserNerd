package mcp

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// startOwnedBackgroundDisplay never reuses DISPLAY or the desktop session.
// It starts its own high-numbered X server and verifies the server's lock PID
// before returning. Missing tools or startup failures abort before Chrome.
func startOwnedBackgroundDisplay(t *testing.T, ctx context.Context) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("headed containment proof requires an owned Linux Xvfb display")
	}
	for _, tool := range []string{"Xvfb", "openbox", "xprop", "xmessage"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("isolated headed verification requires %s: %v", tool, err)
		}
	}
	n, err := rand.Int(rand.Reader, big.NewInt(40000))
	if err != nil {
		t.Fatal(err)
	}
	displayNumber := int(n.Int64()) + 10000
	display := fmt.Sprintf(":%d", displayNumber)
	lockPath := fmt.Sprintf("/tmp/.X%d-lock", displayNumber)
	socketPath := fmt.Sprintf("/tmp/.X11-unix/X%d", displayNumber)
	for _, path := range []string{lockPath, socketPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("refusing occupied or unverifiable display %s: %s", display, path)
		}
	}
	logFile, err := os.Create(filepath.Join(t.TempDir(), "display.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logFile.Close() })
	xserver := exec.Command("Xvfb", display, "-screen", "0", "1280x1024x24", "-nolisten", "tcp", "-ac")
	xserver.Stdout, xserver.Stderr = logFile, logFile
	if err := xserver.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopOwnedDisplayCommand(xserver) })
	owned := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		lock, err := os.ReadFile(lockPath)
		if err == nil && strings.TrimSpace(string(lock)) == strconv.Itoa(xserver.Process.Pid) {
			if _, err := os.Lstat(socketPath); err == nil {
				owned = true
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !owned {
		t.Fatalf("dedicated display %s did not prove ownership by Xvfb PID %d", display, xserver.Process.Pid)
	}
	t.Setenv("DISPLAY", display)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("XDG_SESSION_TYPE", "x11")
	t.Setenv("XAUTHORITY", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	wm := exec.Command("openbox")
	wm.Stdout, wm.Stderr = logFile, logFile
	if err := wm.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopOwnedDisplayCommand(wm) })
	ready := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		out, err := exec.CommandContext(ctx, "xprop", "-root", "_NET_SUPPORTING_WM_CHECK").CombinedOutput()
		if err == nil && strings.Contains(string(out), "0x") {
			ready = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatal("owned display window manager did not become ready; refusing browser launch")
	}
	t.Logf("owned isolated DISPLAY=%s Xvfb_PID=%d lock=%s WAYLAND_DISPLAY cleared; X11 forced for Chrome", display, xserver.Process.Pid, lockPath)
}

func stopOwnedDisplayCommand(cmd *exec.Cmd) {
	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}

func TestOwnedBackgroundDisplay(t *testing.T) {
	if os.Getenv("BROWSERNERD_BACKGROUND_HEADED") != "1" {
		t.Skip("set BROWSERNERD_BACKGROUND_HEADED=1 for owned display containment proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	startOwnedBackgroundDisplay(t, ctx)
	if os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("DISPLAY") == ":0" {
		t.Fatal("owned display leaked desktop routing")
	}
}
