//go:build !windows

package browser

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type unixProcessQuerier struct{}

func DefaultProcessQuerier() ProcessQuerier {
	return &unixProcessQuerier{}
}

func (q *unixProcessQuerier) QueryChromeProcesses(ctx context.Context) ([]ProcessInfo, error) {
	if _, err := os.Stat("/proc"); err == nil {
		return queryLinuxProc()
	}

	cmd := exec.CommandContext(ctx, "ps", "-ax", "-o", "pid,ppid,command")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parsePsOutput(string(out)), nil
}

func queryLinuxProc() ([]ProcessInfo, error) {
	matches, err := filepath.Glob("/proc/[0-9]*/cmdline")
	if err != nil {
		return nil, err
	}
	var results []ProcessInfo
	for _, cmdPath := range matches {
		rawCmd, err := os.ReadFile(cmdPath)
		if err != nil {
			continue
		}
		cmdline := string(bytes.ReplaceAll(rawCmd, []byte{0}, []byte{' '}))
		lower := strings.ToLower(cmdline)
		if !strings.Contains(lower, "chrome") && !strings.Contains(lower, "chromium") {
			continue
		}

		pidStr := filepath.Base(filepath.Dir(cmdPath))
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}

		ppid := readLinuxPPID(pid)
		results = append(results, ProcessInfo{
			PID:         pid,
			PPID:        ppid,
			CommandLine: cmdline,
		})
	}
	return results, nil
}

func readLinuxPPID(pid int) int {
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(statPath)
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) > 3 {
		ppid, err := strconv.Atoi(fields[3])
		if err == nil {
			return ppid
		}
	}
	return 0
}

func parsePsOutput(out string) []ProcessInfo {
	var results []ProcessInfo
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "PID") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		cmdline := strings.Join(fields[2:], " ")
		lower := strings.ToLower(cmdline)
		if strings.Contains(lower, "chrome") || strings.Contains(lower, "chromium") {
			results = append(results, ProcessInfo{
				PID:         pid,
				PPID:        ppid,
				CommandLine: cmdline,
			})
		}
	}
	return results
}

func (q *unixProcessQuerier) IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func (q *unixProcessQuerier) KillProcessTree(pid int) error {
	if pid <= 0 {
		return nil
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	_ = syscall.Kill(pid, syscall.SIGTERM)
	time.Sleep(50 * time.Millisecond)
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
	return nil
}
