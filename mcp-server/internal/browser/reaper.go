package browser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod/lib/launcher"
)

// ProcessInfo represents a running process with command-line information.
type ProcessInfo struct {
	PID         int    `json:"ProcessId"`
	PPID        int    `json:"ParentProcessId"`
	CommandLine string `json:"CommandLine"`
}

// ProcessQuerier abstracts process inspection and termination for cross-platform support and testing.
type ProcessQuerier interface {
	QueryChromeProcesses(ctx context.Context) ([]ProcessInfo, error)
	IsProcessAlive(pid int) bool
	KillProcessTree(pid int) error
}

// extractUserDataDir parses the value of --user-data-dir from a command line.
func extractUserDataDir(cmdLine string) string {
	if cmdLine == "" {
		return ""
	}
	const flagEq = "--user-data-dir="
	const flagSp = "--user-data-dir "

	idx := strings.Index(cmdLine, flagEq)
	valStart := 0
	if idx >= 0 {
		valStart = idx + len(flagEq)
	} else {
		idx = strings.Index(cmdLine, flagSp)
		if idx >= 0 {
			valStart = idx + len(flagSp)
			for valStart < len(cmdLine) && cmdLine[valStart] == ' ' {
				valStart++
			}
		} else {
			return ""
		}
	}

	if valStart >= len(cmdLine) {
		return ""
	}

	rem := cmdLine[valStart:]
	if strings.HasPrefix(rem, "\"") {
		rem = rem[1:]
		end := strings.Index(rem, "\"")
		if end >= 0 {
			return rem[:end]
		}
		return rem
	}
	if strings.HasPrefix(rem, "'") {
		rem = rem[1:]
		end := strings.Index(rem, "'")
		if end >= 0 {
			return rem[:end]
		}
		return rem
	}

	end := strings.Index(rem, " ")
	if end >= 0 {
		return rem[:end]
	}
	return rem
}

// isBrowserNERDUserDataDir checks whether a directory path points to a
// BrowserNERD / Rod temp user-data directory (never matches personal Chrome profiles).
func isBrowserNERDUserDataDir(dir string) bool {
	if dir == "" {
		return false
	}
	clean := strings.ToLower(filepath.Clean(dir))

	prefix := strings.ToLower(filepath.Clean(launcher.DefaultUserDataDirPrefix))
	if prefix != "" && clean == prefix {
		return false // Protect the parent rod/user-data directory itself from deletion
	}

	sep := string(filepath.Separator)
	if strings.Contains(clean, "rod"+sep+"user-data"+sep) ||
		strings.Contains(clean, "rod/user-data/") ||
		strings.Contains(clean, "browsernerd"+sep+"user-data"+sep) ||
		strings.Contains(clean, "browsernerd/user-data/") {
		return true
	}

	if prefix != "" && (strings.HasPrefix(clean, prefix+sep) || strings.HasPrefix(clean, prefix+"/")) {
		return true
	}

	return false
}

// removeUserDataDir safely removes a BrowserNERD / Rod temp user-data directory with retry.
func removeUserDataDir(dir string) error {
	if dir == "" {
		return nil
	}
	if !isBrowserNERDUserDataDir(dir) {
		return fmt.Errorf("safety refusal: path %q is not a BrowserNERD/rod temp user-data dir", dir)
	}
	var err error
	deadline := time.Now().Add(5 * time.Second)
	for {
		err = os.RemoveAll(dir)
		if err == nil || os.IsNotExist(err) {
			return nil
		}
		if !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// SetProcessQuerier overrides the process querier (used by tests).
func (m *SessionManager) SetProcessQuerier(q ProcessQuerier) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.querier = q
}

// ReapedOrphans returns the count of orphaned Chrome trees reaped on startup.
func (m *SessionManager) ReapedOrphans() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.reapedOrphans
}

// ReapOrphans scans for running Chrome process trees that carry a BrowserNERD / Rod
// user-data directory and whose launching server process is dead. It terminates the
// process trees and deletes their temp user-data directories.
func (m *SessionManager) ReapOrphans(ctx context.Context) (int, error) {
	m.mu.Lock()
	if m.querier == nil {
		m.querier = DefaultProcessQuerier()
	}
	q := m.querier
	m.mu.Unlock()

	procs, err := q.QueryChromeProcesses(ctx)
	if err != nil {
		return 0, fmt.Errorf("query chrome processes: %w", err)
	}

	type treeInfo struct {
		userDataDir string
		procs       []ProcessInfo
	}
	trees := make(map[string]*treeInfo)
	for _, p := range procs {
		dir := extractUserDataDir(p.CommandLine)
		if dir == "" || !isBrowserNERDUserDataDir(dir) {
			continue
		}
		cleanDir := strings.ToLower(filepath.Clean(dir))
		if trees[cleanDir] == nil {
			trees[cleanDir] = &treeInfo{
				userDataDir: dir,
				procs:       nil,
			}
		}
		trees[cleanDir].procs = append(trees[cleanDir].procs, p)
	}

	reapedCount := 0
	currentPID := os.Getpid()

	for _, tree := range trees {
		treePIDs := make(map[int]bool)
		for _, p := range tree.procs {
			treePIDs[p.PID] = true
		}

		var rootPPIDs []int
		for _, p := range tree.procs {
			if !treePIDs[p.PPID] {
				rootPPIDs = append(rootPPIDs, p.PPID)
			}
		}

		launcherAlive := false
		for _, ppid := range rootPPIDs {
			if ppid == currentPID {
				launcherAlive = true
				break
			}
			// On Unix, PPID 1 means orphaned/init, not a live launcher server.
			if ppid <= 1 {
				continue
			}
			if q.IsProcessAlive(ppid) {
				launcherAlive = true
				break
			}
		}

		if launcherAlive {
			continue
		}

		// Reaping orphaned tree: kill all processes in this tree
		for _, p := range tree.procs {
			_ = q.KillProcessTree(p.PID)
		}

		// Wait briefly for all processes in tree to exit
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			allDead := true
			for _, p := range tree.procs {
				if q.IsProcessAlive(p.PID) {
					allDead = false
					break
				}
			}
			if allDead {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}

		if tree.userDataDir != "" && isBrowserNERDUserDataDir(tree.userDataDir) {
			_ = removeUserDataDir(tree.userDataDir)
		}

		reapedCount++
	}

	m.mu.Lock()
	m.reapedOrphans += reapedCount
	m.mu.Unlock()

	return reapedCount, nil
}
