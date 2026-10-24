//go:build unix

package logs

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestFilesFIFOIsRefusedWithoutOpening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipe.log")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	entries, err := NewFiles([]File{{Name: "pipe", Path: path}}, time.Second, 4096, 10).LogsBetween(ctx, time.Time{}, time.Time{})
	if len(entries) != 0 || err == nil || time.Since(start) >= time.Second {
		t.Fatalf("FIFO must be refused immediately: %+v, %v", entries, err)
	}
}
