package browser

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestRemoveUserDataDirWaitsForWindowsFileHandles(t *testing.T) {
	profileDir := filepath.Join(t.TempDir(), "rod", "user-data", "test-profile")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(profileDir, "locked-profile-data")
	fileName, err := syscall.UTF16PtrFromString(filePath)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(fileName, syscall.GENERIC_WRITE, 0, nil, syscall.CREATE_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan error, 1)
	go func() {
		time.Sleep(500 * time.Millisecond)
		released <- syscall.CloseHandle(handle)
	}()
	removeErr := removeUserDataDir(profileDir)
	if closeErr := <-released; closeErr != nil {
		t.Fatal(closeErr)
	}
	if removeErr != nil {
		t.Fatalf("profile cleanup did not wait for its file handle: %v", removeErr)
	}
	if _, err := os.Stat(profileDir); !os.IsNotExist(err) {
		t.Fatalf("profile remains after cleanup: %v", err)
	}
}
