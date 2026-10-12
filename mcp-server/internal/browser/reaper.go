package browser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-rod/rod/lib/launcher"
)

// ProcessInfo represents a running process with command-line information.
type ProcessInfo struct {
	PID         int    `json:"ProcessId"`
	CommandLine string `json:"CommandLine"`
}

// ProfileOwnerFile, written into the temporary profile of every Chrome this
// server launches, holds the server's process id.
const ProfileOwnerFile = "browsernerd-owner"

// markProfileOwner records this server as the owner of a launched profile.
func markProfileOwner(dir string) error {
	return os.WriteFile(filepath.Join(dir, ProfileOwnerFile), []byte(strconv.Itoa(os.Getpid())), 0o600)
}

// profileOwner returns the process id of the server that launched the Chrome
// using dir, or 0 when the profile carries no BrowserNERD owner mark.
func profileOwner(dir string) int {
	raw, err := os.ReadFile(filepath.Join(dir, ProfileOwnerFile))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
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
// BrowserNERD / Rod temp user-data directory (never matches personal Chrome
// profiles). Paths compare with forward slashes and case folded, so a profile
// path reads the same whichever platform wrote it.
func isBrowserNERDUserDataDir(dir string) bool {
	if dir == "" {
		return false
	}
	normalize := func(p string) string {
		return strings.ToLower(strings.ReplaceAll(filepath.Clean(p), `\`, "/"))
	}
	clean := normalize(dir)
	prefix := normalize(launcher.DefaultUserDataDirPrefix)
	if clean == prefix {
		return false // Protect the parent rod/user-data directory itself from deletion
	}
	return strings.Contains(clean, "rod/user-data/") ||
		strings.Contains(clean, "browsernerd/user-data/") ||
		strings.HasPrefix(clean, prefix+"/")
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

// ReapOrphans terminates Chrome process trees whose owning BrowserNERD server
// has died and deletes their temporary profiles.
//
// Ownership comes from the profile's owner mark, not the process tree: on
// Linux an orphaned Chrome is adopted by init or a subreaper such as
// "systemd --user", so its parent is alive although its server is gone. A
// profile without a mark was not launched by BrowserNERD (another Rod program,
// or a personal browser) and is never touched.
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
			trees[cleanDir] = &treeInfo{userDataDir: dir}
		}
		trees[cleanDir].procs = append(trees[cleanDir].procs, p)
	}

	reapedCount := 0
	currentPID := os.Getpid()

	for _, tree := range trees {
		owner := profileOwner(tree.userDataDir)
		if owner == 0 || owner == currentPID || q.IsProcessAlive(owner) {
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
