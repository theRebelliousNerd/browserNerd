# Mangle Fact Engine

# ⚠️ CRITICAL ARCHITECTURAL BOUNDARY ⚠️

**ABSOLUTE ISOLATION REQUIREMENT**

This code is part of `dev_tools/` - development utilities for BUILDING SymbioGen.

**NEVER EVER EVER:**
- ❌ Import from `ai_engine/`
- ❌ Import from `api/`
- ❌ Import from `common/`
- ❌ Import from `workers/`
- ❌ Import from `ingestion/`
- ❌ Import from `graph_rag_service/`
- ❌ Import from `frontend/`
- ❌ Use SymbioGen's ArangoDB (localhost:8529, database: symbiogen_db)
- ❌ Use SymbioGen's database credentials
- ❌ Call SymbioGen's API endpoints (localhost:8000)
- ❌ Share any SymbioGen runtime resources

**WHY:** `dev_tools/` is scaffolding. Once built, the scaffolding comes down. The cathedral (SymbioGen) stands alone. They MUST remain completely isolated.

**IF YOU NEED SHARED CODE:** Extract it to a standalone library or duplicate it. Never create dependencies on SymbioGen runtime code.

**dev_tools has its own resources:**
- Separate ArangoDB instance (localhost:8530, database: code_graph)
- Separate credentials
- Standalone Go binaries
- Independent MCP servers

---


`internal/mangle/` wraps Google's Mangle deductive database for browser event reasoning and logic-based assertions.

## File Overview

| File | Purpose | Key Exports |
|------|---------|-------------|
| **`engine.go`** | Mangle engine wrapper | `Engine`, `Fact`, `QueryResult` |
| **`engine_test.go`** | Unit tests | Test cases |

## Engine Architecture

```go
type Engine struct {
    cfg          config.MangleConfig
    programInfo  *analysis.ProgramInfo  // Compiled schema
    store        factstore.FactStore     // Mangle fact storage
    facts        []Fact                  // Temporal buffer
    index        map[string][]int        // Predicate index

    // Adaptive sampling
    samplingRate       float64
    predicateCounts    map[string]int
    lowValuePredicates map[string]bool

    // Watch subscriptions
    subscriptions map[string][]chan WatchEvent
}

type Fact struct {
    Predicate string        `json:"predicate"`
    Args      []interface{} `json:"args"`
    Timestamp time.Time     `json:"timestamp"`
}

type WatchEvent struct {
    Predicate string `json:"predicate"`
    Facts     []Fact `json:"facts"`
    Timestamp time.Time
}
```

## Core Operations

### Add Facts

```go
facts := []mangle.Fact{
    {Predicate: "net_request", Args: []interface{}{"req-1", "GET", "/api/users", "", 1732481234}},
    {Predicate: "console_event", Args: []interface{}{"error", "TypeError", 1732481235}},
}
err := engine.AddFacts(ctx, facts)
```

### Query Facts

```go
results, err := engine.Query(`caused_by(Error, ReqId).`)
// Returns: [{Error: "TypeError", ReqId: "req-1"}]
```

### Temporal Query

```go
facts := engine.QueryTemporal("net_request", afterMs, beforeMs)
```

### Watch Mode

```go
ch := engine.Subscribe("test_passed")
select {
case event := <-ch:
    // Predicate derived new facts
case <-time.After(30 * time.Second):
    // Timeout
}
```

## Adaptive Sampling

Under load, low-value predicates are sampled to prevent buffer overflow:

```go
// Low-value (can be sampled)
"dom_node", "dom_attr", "react_prop", "react_state", "net_header"

// High-value (never sampled)
"console_event", "net_request", "net_response", "navigation_event"
```

Sampling rate adjusts based on fact ingestion rate.

## Predicate Index

Facts are indexed by predicate for O(m) lookup:

```go
index["net_request"] = []int{0, 5, 12, ...}  // Positions in facts slice
```

## Schema Loading

```go
engine.LoadSchema("schemas/browser.mg")
```

Loads Mangle schema with:
- EDB declarations (base facts)
- IDB rules (derived facts)
- Built-in functions

## Key Derived Rules

From `schemas/browser.mg`:

```mangle
# API-triggered crash detection
caused_by(ConsoleErr, ReqId) :-
    console_event("error", ConsoleErr, TError),
    net_response(ReqId, Status, _, _),
    Status >= 400,
    TNet < TError.

# Slow API detection
slow_api(ReqId, Url, Duration) :-
    net_request(ReqId, _, Url, _, _),
    net_response(ReqId, _, _, Duration),
    Duration > 1000.

# Test assertion
test_passed() :-
    navigation_event(_, "/dashboard", _).
```

## Dependencies

- `github.com/google/mangle/engine` - Core Mangle
- `github.com/google/mangle/analysis` - Schema analysis
- `github.com/google/mangle/factstore` - Fact storage
- `github.com/google/mangle/parse` - Schema parsing
