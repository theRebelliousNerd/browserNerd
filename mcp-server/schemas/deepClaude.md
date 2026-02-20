# Mangle Schemas

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


`schemas/` contains Mangle schema definitions for browser intelligence and causal reasoning.

## Schema Files

| Schema | Purpose |
|--------|---------|
| **`browser.mg`** | Core browser predicates and reasoning rules |

## Schema Structure (browser.mg)

The schema implements the four PRD vectors:

### Vector 1: React Fiber Reification

```mangle
# Component tree from __reactFiber keys
Decl react_component(FiberId, ComponentName, ParentFiberId).
Decl react_prop(FiberId, PropKey, PropValue).
Decl react_state(FiberId, HookIndex, Value).
Decl dom_mapping(FiberId, DomNodeId).
```

### Vector 2: Flight Recorder (CDP Events)

```mangle
# DOM Structure
Decl dom_node(NodeId, Tag, Text, ParentId).
Decl dom_attr(NodeId, Key, Value).
Decl dom_text(NodeId, Text).
Decl dom_updated(SessionId, Timestamp).

# Network Events (HAR-like)
Decl net_request(Id, Method, Url, InitiatorId, StartTime).
Decl net_response(Id, Status, Latency, Duration).
Decl net_header(Id, Kind, Key, Value).
Decl request_initiator(Id, Type, ScriptId).

# Browser Events
Decl console_event(Level, Message, Timestamp).
Decl click_event(NodeId, Timestamp).
Decl input_event(NodeId, Value, Timestamp).
Decl state_change(Name, Value, Timestamp).
Decl navigation_event(SessionId, Url, Timestamp).
```

### Vector 3: Session State

```mangle
Decl current_url(SessionId, Url).
```

### Vector 4: Causal Reasoning Rules

```mangle
# Derived predicates
Decl caused_by(ConsoleMessage, RequestId).
Decl slow_api(RequestId, Url, Duration).
Decl cascading_failure(ChildReqId, ParentReqId).
Decl race_condition_detected().
Decl test_passed().
Decl failed_request(RequestId, Url, Status).
Decl error_chain(ConsoleErr, RequestId, Url, Status).
```

## Key Rules

### API-Triggered Crash Detection

```mangle
caused_by(ConsoleErr, ReqId) :-
    console_event("error", ConsoleErr, TError),
    net_response(ReqId, Status, _, _),
    net_request(ReqId, _, _, _, TNet),
    Status >= 400,
    TNet < TError,
    fn:minus(TError, TNet) < 100.
```

### Slow API Detection

```mangle
slow_api(ReqId, Url, Duration) :-
    net_request(ReqId, _, Url, _, _),
    net_response(ReqId, _, _, Duration),
    Duration > 1000.
```

### Test Assertion

```mangle
test_passed() :-
    navigation_event(_, "/dashboard", _).
```

### Failed Request Detection

```mangle
failed_request(ReqId, Url, Status) :-
    net_request(ReqId, _, Url, _, _),
    net_response(ReqId, Status, _, _),
    Status >= 400.
```

## Usage

Load schema in engine:

```go
engine.LoadSchema("schemas/browser.mg")
```

Query derived facts:

```go
results, _ := engine.Query(`caused_by(Error, ReqId).`)
results, _ := engine.Query(`slow_api(ReqId, Url, Duration).`)
results, _ := engine.Query(`test_passed().`)
```

## Adding Custom Rules

Submit rules at runtime via MCP tool:

```json
{
  "tool": "submit-rule",
  "arguments": {
    "rule": "login_success() :- navigation_event(_, \"/dashboard\", _)."
  }
}
```
