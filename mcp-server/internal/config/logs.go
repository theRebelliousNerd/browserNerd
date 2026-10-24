package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	MaxLogWindow    = time.Hour
	MaxLogReadBytes = 64 << 20
	MaxLogEntries   = 100000
)

// LogsConfig controls bounded native file log correlation.
type LogsConfig struct {
	Files        []LogFileConfig `yaml:"files"`
	LogWindow    string          `yaml:"log_window"`
	MaxReadBytes int64           `yaml:"max_read_bytes"`
	MaxEntries   int             `yaml:"max_entries"`
}

// LogFileConfig identifies one file source. AllowedRoot is set by the loader
// for untrusted workspace files; readers must recheck it each time they open.
type LogFileConfig struct {
	Name        string `yaml:"name"`
	Path        string `yaml:"path"`
	AllowedRoot string `yaml:"-"`
}

func defaultLogsConfig() LogsConfig {
	return LogsConfig{LogWindow: "30s", MaxReadBytes: 1 << 20, MaxEntries: 2000}
}

// UnmarshalYAML rejects unsupported native log options, including commands,
// while preserving values supplied by earlier configuration layers.
func (l *LogsConfig) UnmarshalYAML(node *yaml.Node) error {
	if err := logMappingFields(node, "logs", "files", "log_window", "max_read_bytes", "max_entries"); err != nil {
		return err
	}
	type plain LogsConfig
	decoded := plain(*l)
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i+1].Tag == "!!null" {
			return fmt.Errorf("logs.%s must not be null", node.Content[i].Value)
		}
	}
	*l = LogsConfig(decoded)
	return l.validateConfigured()
}

func (f *LogFileConfig) UnmarshalYAML(node *yaml.Node) error {
	if err := logMappingFields(node, "logs.files", "name", "path"); err != nil {
		return err
	}
	type plain LogFileConfig
	var decoded plain
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*f = LogFileConfig(decoded)
	return nil
}

func logMappingFields(node *yaml.Node, field string, allowed ...string) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("%s must be a mapping", field)
	}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		known := false
		for _, name := range allowed {
			known = known || key == name
		}
		if !known {
			return fmt.Errorf("unknown %s option %q", field, key)
		}
	}
	return nil
}

// Validate refuses invalid configured bounds and ambiguous file identities.
func (l *LogsConfig) Validate() error {
	// A zero-valued Config has no native source; loaded configurations start
	// from defaultLogsConfig, so explicit zero limits are still rejected.
	if len(l.Files) == 0 && l.LogWindow == "" && l.MaxReadBytes == 0 && l.MaxEntries == 0 {
		return nil
	}
	return l.validateConfigured()
}

func (l *LogsConfig) validateConfigured() error {
	window, err := time.ParseDuration(l.LogWindow)
	if err != nil || window <= 0 || window > MaxLogWindow {
		return fmt.Errorf("logs.log_window must be a duration > 0 and <= %s", MaxLogWindow)
	}
	if l.MaxReadBytes <= 0 || l.MaxReadBytes > MaxLogReadBytes {
		return fmt.Errorf("logs.max_read_bytes must be > 0 and <= %d", MaxLogReadBytes)
	}
	if l.MaxEntries <= 0 || l.MaxEntries > MaxLogEntries {
		return fmt.Errorf("logs.max_entries must be > 0 and <= %d", MaxLogEntries)
	}
	names, paths := map[string]bool{}, map[string]bool{}
	for i := range l.Files {
		file := &l.Files[i]
		file.Name = strings.TrimSpace(file.Name)
		file.Path = strings.TrimSpace(file.Path)
		if file.Name == "" || file.Path == "" {
			return fmt.Errorf("logs.files[%d] requires a nonblank name and path", i)
		}
		if names[file.Name] {
			return fmt.Errorf("logs.files has duplicate name %q", file.Name)
		}
		path := filepath.Clean(file.Path)
		if paths[path] {
			return fmt.Errorf("logs.files has duplicate path %q", file.Path)
		}
		names[file.Name], paths[path] = true, true
	}
	return nil
}

// GetLogWindow returns the correlation window, with the default for an unset
// config. Startup validation refuses invalid configured durations.
func (l LogsConfig) GetLogWindow() time.Duration {
	window, err := time.ParseDuration(l.LogWindow)
	if err != nil || window <= 0 || window > MaxLogWindow {
		return 30 * time.Second
	}
	return window
}

func resolveLogPaths(logs LogsConfig, baseDir string) LogsConfig {
	if absolute, err := filepath.Abs(baseDir); err == nil {
		baseDir = absolute
	}
	for i := range logs.Files {
		path := strings.TrimSpace(logs.Files[i].Path)
		if path != "" && !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		logs.Files[i].Path = path
	}
	return logs
}

func confineWorkspaceLogs(logs *LogsConfig, wsDir string, trusted bool) error {
	if trusted || len(logs.Files) == 0 {
		return nil
	}
	root, err := filepath.Abs(wsDir)
	if err != nil {
		return err
	}
	resolvedRoot, err := resolveLogPathPrefix(root, 0)
	if err != nil {
		return fmt.Errorf("resolving workspace root: %w", err)
	}
	for i := range logs.Files {
		file := &logs.Files[i]
		resolved, err := resolveLogPathPrefix(file.Path, 0)
		if err != nil || !logPathWithin(root, file.Path) || !logPathWithin(resolvedRoot, resolved) {
			return fmt.Errorf("logs.files[%d].path must remain inside the workspace (including symlinks)", i)
		}
		file.AllowedRoot = root
	}
	return nil
}

func logPathWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Resolve existing ancestors without requiring the final file to exist. A
// dangling symlink is followed too, so a missing target cannot hide an escape.
func resolveLogPathPrefix(path string, links int) (string, error) {
	if links > 40 {
		return "", fmt.Errorf("too many symlinks in log path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	volume := filepath.VolumeName(abs)
	current := volume + string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(abs[len(volume):], string(filepath.Separator)), string(filepath.Separator))
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return filepath.Join(append([]string{current}, parts[i+1:]...)...), nil
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(current)
			if err != nil {
				return "", err
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(current), target)
			}
			return resolveLogPathPrefix(filepath.Join(append([]string{target}, parts[i+1:]...)...), links+1)
		}
	}
	return current, nil
}
