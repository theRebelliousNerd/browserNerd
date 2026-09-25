package buildinfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStaleSourceNamesSourceNewerThanTheBinary(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "internal", "mcp", "tool.go")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("package mcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Source older than the running (test) binary: current.
	past := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(src, past, past); err != nil {
		t.Fatal(err)
	}
	if stale, _ := StaleSource(root); stale != "" {
		t.Fatalf("source older than the binary reported stale: %s", stale)
	}

	// Source edited after the binary was built: stale, and named.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(src, future, future); err != nil {
		t.Fatal(err)
	}
	stale, _ := StaleSource(root)
	if !strings.HasSuffix(stale, "tool.go") {
		t.Fatalf("newer source not named: %q", stale)
	}

	// Tests and the bin directory never make a binary stale.
	if err := os.Chtimes(src, past, past); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(root, "internal", "mcp", "tool_test.go"), filepath.Join(root, "bin", "gen.go")} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, future, future); err != nil {
			t.Fatal(err)
		}
	}
	if stale, _ := StaleSource(root); stale != "" {
		t.Fatalf("a test file or bin/ made the binary stale: %s", stale)
	}
}

func TestStaleSourceIgnoresAMissingTree(t *testing.T) {
	if stale, _ := StaleSource(filepath.Join(t.TempDir(), "absent")); stale != "" {
		t.Fatalf("missing tree reported stale: %s", stale)
	}
}
