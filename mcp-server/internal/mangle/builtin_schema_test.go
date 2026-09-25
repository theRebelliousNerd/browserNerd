package mangle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
)

// The built-in schema is compiled into the binary: an engine with no
// schema_path loads every built-in module and is ready.
func TestEngineLoadsBuiltinSchemaWithoutPath(t *testing.T) {
	engine, err := NewEngine(config.MangleConfig{Enable: true, FactBufferLimit: 1000})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if !engine.Ready() {
		t.Fatal("engine with the built-in schema must be ready")
	}
	builtin, err := builtinSchemaSources()
	if err != nil {
		t.Fatal(err)
	}
	disk, err := discoverSchemaSources("../../schemas/browser.mg")
	if err != nil {
		t.Fatal(err)
	}
	if len(builtin) != len(disk) {
		t.Fatalf("embedded schema has %d modules, schemas/ on disk has %d", len(builtin), len(disk))
	}
	for i := range builtin {
		if builtin[i].Module() != disk[i].Module() {
			t.Fatalf("module %d: embedded %s, disk %s", i, builtin[i].Module(), disk[i].Module())
		}
	}
}

// A project module adds declarations and rules on top of the built-in ones,
// including extra rules for a built-in predicate.
func TestEngineProjectModulesExtendBuiltinSchema(t *testing.T) {
	dir := t.TempDir()
	module := `Decl project_error(SessionId, Message).
project_error(S, M) :- console_event(S, "error", M, _).
user_visible_error(S, "project", M, T) :- console_event(S, "warning", M, T).
`
	if err := os.WriteFile(filepath.Join(dir, "90_project.mg"), []byte(module), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(config.MangleConfig{Enable: true, SchemaPath: dir, FactBufferLimit: 1000})
	if err != nil {
		t.Fatalf("NewEngine with a project module: %v", err)
	}
	now := time.Now()
	if err := engine.AddFacts(context.Background(), []Fact{
		{Predicate: "console_event", Args: []interface{}{"s1", "error", "boom", now.UnixMilli()}, Timestamp: now},
		{Predicate: "console_event", Args: []interface{}{"s1", "warning", "careful", now.UnixMilli()}, Timestamp: now},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := engine.Query(context.Background(), "project_error(S, M).")
	if err != nil || len(rows) != 1 {
		t.Fatalf("project rule must derive over built-in facts: rows=%v err=%v", rows, err)
	}
	rows, err = engine.Query(context.Background(), `user_visible_error(S, "project", M, T).`)
	if err != nil || len(rows) != 1 {
		t.Fatalf("a project rule may extend a built-in predicate: rows=%v err=%v", rows, err)
	}
}

// Redeclaring a built-in predicate is a module conflict, reported with both
// modules named, not a silent override.
func TestEngineRejectsProjectRedeclarationOfBuiltin(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "monolith.mg"), []byte("Decl net_response(SessionId, Id, Status).\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewEngine(config.MangleConfig{Enable: true, SchemaPath: dir, FactBufferLimit: 1000})
	if err == nil {
		t.Fatal("expected a duplicate declaration error")
	}
	for _, want := range []string{"net_response", "builtin:00_core_events", "project:monolith"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error must name %q: %v", want, err)
		}
	}
}

// A project path that is the built-in schema itself loads each module once.
func TestEngineLoadsBuiltinCopyOnce(t *testing.T) {
	engine, err := NewEngine(config.MangleConfig{Enable: true, SchemaPath: "../../schemas/browser.mg", FactBufferLimit: 1000})
	if err != nil {
		t.Fatalf("identical modules are the same module: %v", err)
	}
	builtin, _ := builtinSchemaSources()
	if got := len(engine.schemaModules); got != len(builtin) {
		t.Fatalf("expected %d modules loaded once, got %d", len(builtin), got)
	}
}

// With the built-in schema disabled and no project modules there is no
// program, and the engine says so instead of answering from nothing.
func TestEngineWithBuiltinDisabledAndNoPathIsNotReady(t *testing.T) {
	engine, err := NewEngine(config.MangleConfig{Enable: true, DisableBuiltin: true, FactBufferLimit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Query(context.Background(), "current_url(S, U)."); err == nil {
		t.Fatal("expected engine not ready")
	}
}
