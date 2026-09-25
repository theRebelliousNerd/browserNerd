// Package buildinfo reports which source a running BrowserNERD binary was
// built from, so a stale binary is named instead of silently misbehaving.
package buildinfo

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
)

// Version is stamped at link time:
//
//	go build -ldflags "-X browsernerd-mcp-server/internal/buildinfo.Version=1.2.0" ./cmd/server
var Version = "dev"

// Info identifies the running binary.
type Info struct {
	Version   string    `json:"version"`
	Revision  string    `json:"revision,omitempty"`
	Modified  bool      `json:"modified"`
	BuildTime time.Time `json:"build_time,omitzero"`
	GoVersion string    `json:"go_version"`
	Binary    string    `json:"binary,omitempty"`
}

// Read returns the running binary's identity from the VCS stamp Go embeds at
// build time. BuildTime is the binary file's modification time, which is when
// it was built.
func Read() Info {
	info := Info{Version: Version}
	if exe, err := os.Executable(); err == nil {
		info.Binary = exe
		if st, statErr := os.Stat(exe); statErr == nil {
			info.BuildTime = st.ModTime()
		}
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	info.GoVersion = bi.GoVersion
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Revision = s.Value
			if len(info.Revision) > 12 {
				info.Revision = info.Revision[:12]
			}
		case "vcs.modified":
			info.Modified = s.Value == "true"
		}
	}
	return info
}

// StaleSource reports the newest Go source file under sourceRoot that is newer
// than the running binary, or "" when the binary is current or the source tree
// is not present. It bounds the walk so a missing or huge tree costs nothing.
func StaleSource(sourceRoot string) (string, time.Time) {
	info := Read()
	if info.BuildTime.IsZero() || sourceRoot == "" {
		return "", time.Time{}
	}
	if st, err := os.Stat(sourceRoot); err != nil || !st.IsDir() {
		return "", time.Time{}
	}
	var newest string
	var newestAt time.Time
	visited := 0
	_ = filepath.WalkDir(sourceRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == "bin" || name == "testdata" || name == "coverage" || name == "data" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		visited++
		if visited > 5000 {
			return filepath.SkipAll
		}
		if !strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".mg") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fi, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if fi.ModTime().After(newestAt) {
			newestAt = fi.ModTime()
			newest = path
		}
		return nil
	})
	// Two seconds of slack absorbs filesystem timestamp granularity.
	if newestAt.After(info.BuildTime.Add(2 * time.Second)) {
		return newest, newestAt
	}
	return "", time.Time{}
}

// SourceRootForBinary guesses the module root of a binary built into
// <root>/bin, the layout this repository uses.
func SourceRootForBinary() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	root := filepath.Dir(filepath.Dir(exe))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return ""
	}
	return root
}
