//go:build windows

package browser

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

type windowsProcessQuerier struct{}

func DefaultProcessQuerier() ProcessQuerier {
	return &windowsProcessQuerier{}
}

func (q *windowsProcessQuerier) QueryChromeProcesses(ctx context.Context) ([]ProcessInfo, error) {
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
		`Get-CimInstance Win32_Process -Filter "Name like 'chrome%.exe' or Name like 'chromium%.exe'" | Select-Object ProcessId, CommandLine | ConvertTo-Json`)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}

	var procs []ProcessInfo
	if err := json.Unmarshal(out, &procs); err != nil {
		var single ProcessInfo
		if err2 := json.Unmarshal(out, &single); err2 == nil && single.PID != 0 {
			procs = []ProcessInfo{single}
		}
	}
	return procs, nil
}

func (q *windowsProcessQuerier) IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := syscall.OpenProcess(syscall.SYNCHRONIZE|syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
			return true
		}
		return false
	}
	defer syscall.CloseHandle(handle)

	event, err := syscall.WaitForSingleObject(handle, 0)
	if err != nil {
		return false
	}
	return event == syscall.WAIT_TIMEOUT
}

func (q *windowsProcessQuerier) KillProcessTree(pid int) error {
	if pid <= 0 {
		return nil
	}
	// 1. Enumerate all descendant PIDs using Toolhelp32 snapshot
	descendants := findChildPIDs(pid)

	// 2. Terminate via taskkill /F /T /PID <pid>
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	_ = cmd.Run()

	// 3. Fallback: explicitly terminate each descendant and root process directly
	for _, p := range append(descendants, pid) {
		if q.IsProcessAlive(p) {
			handle, err := syscall.OpenProcess(syscall.PROCESS_TERMINATE, false, uint32(p))
			if err == nil {
				_ = syscall.TerminateProcess(handle, 1)
				_ = syscall.CloseHandle(handle)
			}
		}
	}
	return nil
}

func findChildPIDs(rootPID int) []int {
	snapshot, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(snapshot)

	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := syscall.Process32First(snapshot, &entry); err != nil {
		return nil
	}

	childrenMap := make(map[int][]int)
	for {
		parent := int(entry.ParentProcessID)
		child := int(entry.ProcessID)
		childrenMap[parent] = append(childrenMap[parent], child)
		if err := syscall.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}

	var allDescendants []int
	queue := []int{rootPID}
	visited := make(map[int]bool)
	visited[rootPID] = true

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		for _, child := range childrenMap[curr] {
			if !visited[child] {
				visited[child] = true
				allDescendants = append(allDescendants, child)
				queue = append(queue, child)
			}
		}
	}
	return allDescendants
}
