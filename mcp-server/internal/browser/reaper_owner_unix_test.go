//go:build !windows

package browser

import (
	"errors"
	"syscall"
	"testing"
)

func TestUnixReaperOnlyConfirmedAbsenceAuthorizesCleanup(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		preserve bool
	}{
		{name: "alive", preserve: true},
		{name: "permission denied", err: syscall.EPERM, preserve: true},
		{name: "access denied", err: syscall.EACCES, preserve: true},
		{name: "unknown failure", err: errors.New("unverified process"), preserve: true},
		{name: "confirmed absent", err: syscall.ESRCH},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &unixProcessQuerier{probeProcess: func(int) error { return tc.err }}
			if got := q.IsProcessAlive(123); got != tc.preserve {
				t.Fatalf("preserve=%v, want %v for %v", got, tc.preserve, tc.err)
			}
		})
	}
}
