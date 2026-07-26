package security

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PathPolicy confines model-supplied writes to explicit local roots.
type PathPolicy struct {
	baseDir string
	roots   []string
}

// DefaultRoot returns the first configured writable root.
func (p *PathPolicy) DefaultRoot() string {
	if p == nil || len(p.roots) == 0 {
		return ""
	}
	return p.roots[0]
}

// NewPathPolicy resolves allowed roots against baseDir.
func NewPathPolicy(baseDir string, roots []string) (*PathPolicy, error) {
	if strings.TrimSpace(baseDir) == "" {
		var err error
		baseDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get path policy base directory: %w", err)
		}
	}
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("resolve path policy base directory: %w", err)
	}
	if len(roots) == 0 {
		roots = []string{"screenshots", filepath.Join("data", "traces")}
	}

	resolved := make([]string, 0, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if !filepath.IsAbs(root) {
			root = filepath.Join(baseAbs, root)
		}
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve writable root %q: %w", root, err)
		}
		resolved = append(resolved, filepath.Clean(rootAbs))
	}
	if len(resolved) == 0 {
		return nil, errors.New("security.writable_roots must contain at least one path")
	}
	return &PathPolicy{baseDir: filepath.Clean(baseAbs), roots: resolved}, nil
}

// ResolveForWrite resolves requested under defaultRoot and verifies that the
// final path, including existing symlink parents, remains inside an allowed root.
func (p *PathPolicy) ResolveForWrite(requested, defaultRoot, defaultName string) (string, error) {
	if p == nil {
		return "", errors.New("write path policy is not configured")
	}
	requested = strings.TrimSpace(requested)
	if requested == "" {
		requested = defaultName
	}
	if !filepath.IsAbs(requested) {
		base := p.baseDir
		if strings.TrimSpace(defaultRoot) != "" {
			base = defaultRoot
			if !filepath.IsAbs(base) {
				base = filepath.Join(p.baseDir, base)
			}
		}
		requested = filepath.Join(base, requested)
	}
	target, err := filepath.Abs(requested)
	if err != nil {
		return "", fmt.Errorf("resolve output path: %w", err)
	}
	target = filepath.Clean(target)

	resolvedTarget, err := resolveExistingPrefix(target)
	if err != nil {
		return "", err
	}
	for _, root := range p.roots {
		resolvedRoot, rootErr := resolveExistingPrefix(root)
		if rootErr != nil {
			return "", rootErr
		}
		if pathWithin(resolvedRoot, resolvedTarget) {
			return target, nil
		}
	}
	return "", fmt.Errorf("output path %q is outside security.writable_roots", target)
}

// EnsurePrivateDir creates a directory that is accessible only to the current
// user on permission-aware platforms.
func EnsurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

// WritePrivateFile writes a file with owner-only permissions.
func WritePrivateFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func pathWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func resolveExistingPrefix(path string) (string, error) {
	current := filepath.Clean(path)
	missing := make([]string, 0, 4)
	for {
		_, err := os.Lstat(current)
		if err == nil {
			resolved, evalErr := filepath.EvalSymlinks(current)
			if evalErr != nil {
				return "", fmt.Errorf("resolve output symlink %q: %w", current, evalErr)
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect output path %q: %w", current, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(path), nil
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}
