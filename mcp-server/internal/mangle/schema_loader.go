package mangle

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"browsernerd-mcp-server/schemas"
)

const schemaIncludePrefix = "# @include "

// Schema module origins: the modules compiled into the binary, and the
// project modules a workspace or config adds on top of them.
const (
	originBuiltin = "builtin"
	originProject = "project"
)

type schemaSource struct {
	Path   string
	Data   []byte
	Origin string // builtin | project
}

// schemaModule names a loaded module for diagnostics and the schema catalog.
type schemaModule struct {
	Name   string `json:"name"`
	Origin string `json:"origin"`
	Path   string `json:"path"`
}

// Label renders the module as origin:name, e.g. builtin:00_core_events.
func (m schemaModule) Label() string {
	return m.Origin + ":" + m.Name
}

// Module is the source's module name: its file name without the .mg extension.
func (s schemaSource) Module() string {
	base := filepath.Base(filepath.FromSlash(s.Path))
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// resolveSchemaSources returns the program's modules in load order: the
// built-in modules (unless disabled), then the project modules at projectPath.
// A project module byte-identical to a built-in module is the same module and
// loads once, exactly as a repeated include does.
func resolveSchemaSources(includeBuiltin bool, projectPath string) ([]schemaSource, error) {
	var sources []schemaSource
	if includeBuiltin {
		builtin, err := builtinSchemaSources()
		if err != nil {
			return nil, err
		}
		sources = append(sources, builtin...)
	}
	if strings.TrimSpace(projectPath) == "" {
		return sources, nil
	}
	project, err := discoverSchemaSources(projectPath)
	if err != nil {
		return nil, err
	}
	for _, candidate := range project {
		duplicate := false
		for _, loaded := range sources {
			if loaded.Origin == originBuiltin && bytes.Equal(loaded.Data, candidate.Data) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			sources = append(sources, candidate)
		}
	}
	return sources, nil
}

// builtinSchemaSources expands the embedded manifest in lexical module order.
func builtinSchemaSources() ([]schemaSource, error) {
	visited := make(map[string]bool)
	var sources []schemaSource
	if err := collectEmbeddedSchemaSources(schemas.FS, schemas.Manifest, visited, &sources); err != nil {
		return nil, fmt.Errorf("built-in schema: %w", err)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	if len(sources) == 0 {
		return nil, fmt.Errorf("built-in schema contains no executable .mg modules")
	}
	return sources, nil
}

func collectEmbeddedSchemaSources(fsys fs.FS, name string, visited map[string]bool, sources *[]schemaSource) error {
	name = path.Clean(name)
	if visited[name] {
		return nil
	}
	visited[name] = true
	info, err := fs.Stat(fsys, name)
	if err != nil {
		return err
	}
	if info.IsDir() {
		entries, err := fs.ReadDir(fsys, name)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.EqualFold(path.Ext(entry.Name()), ".mg") {
				continue
			}
			if err := collectEmbeddedSchemaSources(fsys, path.Join(name, entry.Name()), visited, sources); err != nil {
				return err
			}
		}
		return nil
	}
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return err
	}
	includes, executable, err := inspectSchemaModule(data)
	if err != nil {
		return fmt.Errorf("inspect schema module %s: %w", name, err)
	}
	for _, include := range includes {
		if err := collectEmbeddedSchemaSources(fsys, path.Join(path.Dir(name), include), visited, sources); err != nil {
			return err
		}
	}
	if executable {
		*sources = append(*sources, schemaSource{Path: name, Data: data, Origin: originBuiltin})
	}
	return nil
}

// discoverSchemaSources accepts a single .mg module, a directory of modules, or
// a small manifest containing "# @include relative/path" directives.
func discoverSchemaSources(root string) ([]schemaSource, error) {
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("read schema path %s: %w", root, err)
	}

	trustRoot := root
	if !info.IsDir() {
		trustRoot = filepath.Dir(root)
	}
	trustRoot, err = filepath.Abs(trustRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve schema root: %w", err)
	}

	visited := make(map[string]bool)
	var sources []schemaSource
	if err := collectSchemaSources(root, trustRoot, visited, &sources); err != nil {
		return nil, err
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	if len(sources) == 0 {
		return nil, fmt.Errorf("schema path %s contains no executable .mg modules", root)
	}
	return sources, nil
}

func collectSchemaSources(path, trustRoot string, visited map[string]bool, sources *[]schemaSource) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve schema path %s: %w", path, err)
	}
	if err := requireSchemaPathWithin(absolute, trustRoot); err != nil {
		return err
	}
	if visited[absolute] {
		return nil
	}
	visited[absolute] = true

	info, err := os.Stat(absolute)
	if err != nil {
		return fmt.Errorf("read schema path %s: %w", absolute, err)
	}
	if info.IsDir() {
		var modulePaths []string
		err := filepath.WalkDir(absolute, func(candidate string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".mg") {
				return nil
			}
			modulePaths = append(modulePaths, candidate)
			return nil
		})
		if err != nil {
			return fmt.Errorf("scan schema directory %s: %w", absolute, err)
		}
		sort.Strings(modulePaths)
		for _, modulePath := range modulePaths {
			if err := collectSchemaSources(modulePath, trustRoot, visited, sources); err != nil {
				return err
			}
		}
		return nil
	}
	if !strings.EqualFold(filepath.Ext(absolute), ".mg") {
		return fmt.Errorf("schema file must use .mg extension: %s", absolute)
	}

	data, err := os.ReadFile(absolute)
	if err != nil {
		return fmt.Errorf("read schema module %s: %w", absolute, err)
	}
	includes, executable, err := inspectSchemaModule(data)
	if err != nil {
		return fmt.Errorf("inspect schema module %s: %w", absolute, err)
	}
	for _, include := range includes {
		includePath := filepath.Join(filepath.Dir(absolute), filepath.FromSlash(include))
		if err := collectSchemaSources(includePath, trustRoot, visited, sources); err != nil {
			return err
		}
	}
	if executable {
		*sources = append(*sources, schemaSource{Path: absolute, Data: data, Origin: originProject})
	}
	return nil
}

func inspectSchemaModule(data []byte) ([]string, bool, error) {
	var includes []string
	executable := false
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, schemaIncludePrefix) {
			include := strings.TrimSpace(strings.TrimPrefix(line, schemaIncludePrefix))
			if include == "" || filepath.IsAbs(include) {
				return nil, false, fmt.Errorf("include must be a non-empty relative path")
			}
			includes = append(includes, include)
			continue
		}
		if !strings.HasPrefix(line, "#") {
			executable = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, false, err
	}
	return includes, executable, nil
}

func requireSchemaPathWithin(path, root string) error {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("compare schema path %s to root %s: %w", path, root, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("schema include escapes root %s: %s", root, path)
	}
	return nil
}
