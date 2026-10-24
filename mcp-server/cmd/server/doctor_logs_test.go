package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
)

func TestStandaloneDoctorConnectsNativeLogs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "worker.log")
	if err := os.WriteFile(path, []byte(time.Now().Format(time.RFC3339Nano)+" ERROR WorkerError: contents must stay private password=doctor-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Docker.Enabled = false
	cfg.Logs.Files = []config.LogFileConfig{{Name: "worker", Path: path}}
	// Keep process inventory inside the fixture and avoid unrelated path/env
	// findings so the exit status proves the native source is connected.
	t.Setenv("TMPDIR", dir)
	cfg.Browser.DownloadDir = dir
	cfg.Mangle.SchemaPath = ""
	cfg.Security.WritableRoots = nil
	t.Setenv("BROWSERNERD_TEST_PASSWORD", "fixture-only")
	stdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = stdout; _ = reader.Close(); _ = writer.Close() })
	status := doctor(context.Background(), cfg, "")
	_ = writer.Close()
	os.Stdout = stdout
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Logs struct {
			Configured       bool `json:"configured"`
			Complete         bool `json:"complete"`
			AvailableEntries int  `json:"available_entries"`
		} `json:"logs"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if status != 0 || !report.Logs.Configured || !report.Logs.Complete || report.Logs.AvailableEntries != 1 {
		t.Fatalf("standalone doctor failed to connect the native source (exit %d): %s", status, data)
	}
	if !strings.Contains(string(data), `"name": "worker"`) || strings.Contains(string(data), "WorkerError") || strings.Contains(string(data), "doctor-secret") {
		t.Fatalf("doctor must report source identity without log content: %s", data)
	}
}
