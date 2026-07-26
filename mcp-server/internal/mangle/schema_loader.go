package mangle

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const schemaIncludePrefix = "# @include "

type schemaSource struct {
	Path string
	Data []byte
}

// discoverSchemaSources accepts a legacy .mg file, a directory of modules, or
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
		*sources = append(*sources, schemaSource{Path: absolute, Data: data})
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
