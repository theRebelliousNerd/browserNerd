package browser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type profileOwnership struct {
	PID          int    `json:"pid"`
	ProcessScope string `json:"process_scope"`
}

// processScope qualifies process numbers before they can authorize cleanup.
func processScope() (string, error) {
	if runtime.GOOS != "linux" {
		return runtime.GOOS, nil
	}
	namespace, err := os.Readlink("/proc/self/ns/pid")
	if err != nil {
		return "", fmt.Errorf("PID namespace identity unavailable: %w", err)
	}
	return "linux:" + namespace, nil
}

func markProfileOwner(dir string) error {
	scope, err := processScope()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(profileOwnership{PID: os.Getpid(), ProcessScope: scope})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ProfileOwnerFile), raw, 0o600)
}

// profileOwner returns a PID only when its process scope is ours. Unknown or
// foreign-scope marks never grant authority to signal a tree or remove a profile.
func profileOwner(dir string) int {
	raw, err := os.ReadFile(filepath.Join(dir, ProfileOwnerFile))
	if err != nil {
		return 0
	}
	var owner profileOwnership
	if err := json.Unmarshal(raw, &owner); err != nil || owner.PID <= 0 || owner.ProcessScope == "" {
		return 0
	}
	scope, err := processScope()
	if err != nil || owner.ProcessScope != scope {
		return 0
	}
	return owner.PID
}
