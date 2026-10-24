//go:build !windows

package browser

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// ownedProcessExited is a read-only, tri-state probe. EPERM is unknown, not
// death. Linux zombies cannot hold a live CDP browser or contexts.
func ownedProcessExited(pid int) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("invalid owned process id")
	}
	err := syscall.Kill(pid, 0)
	if errors.Is(err, syscall.ESRCH) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if runtime.GOOS != "linux" {
		return false, nil
	}
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false, err
	}
	end := strings.LastIndex(string(raw), ") ")
	if end < 0 {
		return false, fmt.Errorf("owned process state unavailable")
	}
	fields := strings.Fields(string(raw[end+2:]))
	if len(fields) == 0 {
		return false, fmt.Errorf("owned process state missing")
	}
	return fields[0] == "Z" || fields[0] == "X", nil
}
