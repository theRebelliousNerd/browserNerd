package security

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPathPolicyAllowsOnlyConfiguredRoots(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "artifacts")
	policy, err := NewPathPolicy(base, []string{allowed})
	if err != nil {
		t.Fatal(err)
	}

	got, err := policy.ResolveForWrite("capture.png", allowed, "default.png")
	if err != nil {
		t.Fatalf("expected allowed path: %v", err)
	}
	if got != filepath.Join(allowed, "capture.png") {
		t.Fatalf("unexpected resolved path: %q", got)
	}

	if _, err := policy.ResolveForWrite(filepath.Join(base, "outside.txt"), allowed, "default.txt"); err == nil {
		t.Fatal("expected outside path to be rejected")
	}
	if _, err := policy.ResolveForWrite(filepath.Join("..", "outside.txt"), allowed, "default.txt"); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

func TestPathPolicyRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks may require Windows developer mode")
	}
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(allowed, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(allowed, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	policy, err := NewPathPolicy(base, []string{allowed})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.ResolveForWrite(filepath.Join(link, "secret.txt"), allowed, "default.txt"); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}

func TestPrivateFilePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "trace.jsonl")
	if err := WritePrivateFile(path, []byte("safe")); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 file, got %o", info.Mode().Perm())
	}
}
