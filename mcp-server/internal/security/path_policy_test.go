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

func writeUploadFixture(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResolveForUploadAdmitsWorkspaceFiles(t *testing.T) {
	base := t.TempDir()
	quote := filepath.Join(base, "seedData", "import", "vendor-quote.pdf")
	writeUploadFixture(t, quote, 64)
	policy, err := NewPathPolicy(base, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, requested := range []string{quote, filepath.Join("seedData", "import", "vendor-quote.pdf")} {
		got, err := policy.ResolveForUpload(requested)
		if err != nil {
			t.Fatalf("expected %q to be admitted: %v", requested, err)
		}
		want, _ := filepath.EvalSymlinks(quote)
		if got != want {
			t.Fatalf("resolved %q, want %q", got, want)
		}
	}
}

func TestResolveForUploadRefusesWhatAPageMustNotReceive(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "workspace")
	outside := filepath.Join(root, "elsewhere.csv")
	writeUploadFixture(t, outside, 8)
	writeUploadFixture(t, filepath.Join(base, ".env"), 8)
	writeUploadFixture(t, filepath.Join(base, ".credentials", "gcp.json"), 8)
	writeUploadFixture(t, filepath.Join(base, "configs", "api-token.txt"), 8)
	writeUploadFixture(t, filepath.Join(base, "certs", "server.pem"), 8)
	writeUploadFixture(t, filepath.Join(base, "big.bin"), int(MaxUploadBytes)+1)
	if err := os.MkdirAll(filepath.Join(base, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	policy, err := NewPathPolicy(base, nil)
	if err != nil {
		t.Fatal(err)
	}

	for name, requested := range map[string]string{
		"outside the workspace": outside,
		"traversal":             filepath.Join("..", "elsewhere.csv"),
		"dotfile":               ".env",
		"dot directory":         filepath.Join(".credentials", "gcp.json"),
		"credential name":       filepath.Join("configs", "api-token.txt"),
		"key file":              filepath.Join("certs", "server.pem"),
		"over the size limit":   "big.bin",
		"directory":             "folder",
		"missing":               "absent.csv",
		"empty":                 "  ",
	} {
		if got, err := policy.ResolveForUpload(requested); err == nil {
			t.Errorf("%s: expected %q to be refused, got %q", name, requested, got)
		}
	}

	var nilPolicy *PathPolicy
	if _, err := nilPolicy.ResolveForUpload("anything.csv"); err == nil {
		t.Error("expected an unconfigured policy to refuse every upload")
	}
}

func TestResolveForUploadRefusesSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks may require Windows developer mode")
	}
	root := t.TempDir()
	base := filepath.Join(root, "workspace")
	secret := filepath.Join(root, "outside", "notes.txt")
	writeUploadFixture(t, secret, 8)
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "notes.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	policy, err := NewPathPolicy(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.ResolveForUpload(link); err == nil {
		t.Fatal("expected a symlink out of the workspace to be refused")
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
