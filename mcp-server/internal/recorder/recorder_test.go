package recorder

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecorderRotation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "recorder_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	r, err := NewRecorder(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	// Create more than MaxRotatedFiles
	for i := 0; i < MaxRotatedFiles+2; i++ {
		err := r.Start("test")
		if err != nil {
			t.Fatal(err)
		}
		r.Log("test", "sess", map[string]string{"msg": "hello"})
		time.Sleep(10 * time.Millisecond) // Ensure different mod times
	}

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	// We should only have MaxRotatedFiles
	if len(entries) != MaxRotatedFiles {
		t.Errorf("expected %d files, got %d", MaxRotatedFiles, len(entries))
	}
}

func TestRecorderLogging(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "recorder_log_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	r, err := NewRecorder(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	err = r.Start("session1")
	if err != nil {
		t.Fatal(err)
	}

	r.Log("console", "session1", "test message")
	r.Close()

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}

	content, err := os.ReadFile(filepath.Join(tempDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}

	if !filepath.HasPrefix(string(content), `{"ts":`) {
		t.Errorf("unexpected log content format: %s", string(content))
	}
}
