//go:build !windows

package browser

import (
	"bufio"
	"bytes"
	"context"
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

	cmd := exec.CommandContext(ctx, "ps", "-ax", "-o", "pid,command")
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

		results = append(results, ProcessInfo{PID: pid, CommandLine: cmdline})
	}
	return results, nil
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
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		cmdline := strings.Join(fields[1:], " ")
		lower := strings.ToLower(cmdline)
		if strings.Contains(lower, "chrome") || strings.Contains(lower, "chromium") {
			results = append(results, ProcessInfo{PID: pid, CommandLine: cmdline})
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
