//go:build windows

package browser

import (
	"errors"
	"fmt"
	"syscall"
)

func ownedProcessExited(pid int) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("invalid owned process id")
	}
	handle, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, syscall.Errno(87)) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	defer syscall.CloseHandle(handle)
	state, err := syscall.WaitForSingleObject(handle, 0)
	if err != nil {
		return false, err
	}
	switch state {
	case syscall.WAIT_OBJECT_0:
		return true, nil
	case syscall.WAIT_TIMEOUT:
		return false, nil
	default:
		return false, fmt.Errorf("owned process wait status unverified: %d", state)
	}
}
