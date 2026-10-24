package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
)

func TestDoctorReadOnlyChromeFixture(t *testing.T) {
	if os.Getenv("BROWSERNERD_DOCTOR_FIXTURE") != "1" {
		return
	}
	fmt.Println("fixture ready")
	time.Sleep(time.Minute)
}

func TestCLIDoctorDoesNotReapOrDeleteProfiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rod", "user-data", "doctor-fixture")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	namespace, err := os.Readlink("/proc/self/ns/pid")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]interface{}{"pid": 1073741824, "process_scope": "linux:" + namespace})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, browser.ProfileOwnerFile), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDoctorReadOnlyChromeFixture$", "--", "chrome", "--user-data-dir="+dir)
	cmd.Env = append(os.Environ(), "BROWSERNERD_DOCTOR_FIXTURE=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	ready := make(chan string, 1)
	go func() { scanner := bufio.NewScanner(output); scanner.Scan(); ready <- scanner.Text() }()
	select {
	case text := <-ready:
		if text != "fixture ready" {
			t.Fatalf("fixture not ready: %q", text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fixture startup timed out")
	}
	_ = doctor(context.Background(), config.DefaultConfig(), "")
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("Doctor terminated fixture: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("Doctor deleted fixture profile: %v", err)
	}
}
