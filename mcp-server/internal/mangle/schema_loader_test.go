package mangle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"browsernerd-mcp-server/internal/config"
)

func TestDiscoverSchemaSourcesDirectoryAndManifest(t *testing.T) {
	root := t.TempDir()
	modules := filepath.Join(root, "modules")
	if err := os.Mkdir(modules, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modules, "20_second.mg"), []byte("Decl second(X).\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modules, "10_first.mg"), []byte("Decl first(X).\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "browser.mg")
	if err := os.WriteFile(manifest, []byte("# @include modules\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	sources, err := discoverSchemaSources(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected two modules, got %+v", sources)
	}
	if !strings.HasSuffix(sources[0].Path, "10_first.mg") ||
		!strings.HasSuffix(sources[1].Path, "20_second.mg") {
		t.Fatalf("modules were not ordered deterministically: %+v", sources)
	}
}

func TestDiscoverSchemaSourcesRejectsEmptyDirectory(t *testing.T) {
	_, err := discoverSchemaSources(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no executable .mg modules") {
		t.Fatalf("expected empty module error, got %v", err)
	}
}

func TestDiscoverSchemaSourcesRejectsEscapingInclude(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "schemas")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "outside.mg"), []byte("Decl outside(X).\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "browser.mg")
	if err := os.WriteFile(manifest, []byte("# @include ../outside.mg\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := discoverSchemaSources(manifest)
	if err == nil || !strings.Contains(err.Error(), "escapes root") {
		t.Fatalf("expected include confinement error, got %v", err)
	}
}

func TestEngineLoadsSchemaDirectory(t *testing.T) {
	engine, err := NewEngine(testMangleConfig("../../schemas/modules"))
	if err != nil {
		t.Fatalf("load modular schema: %v", err)
	}
	if !engine.Ready() {
		t.Fatal("engine not ready after modular schema load")
	}
}

func testMangleConfig(path string) config.MangleConfig {
	return config.MangleConfig{
		Enable:          true,
		SchemaPath:      path,
		FactBufferLimit: 1000,
	}
}
