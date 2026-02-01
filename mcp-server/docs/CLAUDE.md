# BrowserNERD MCP Server Documentation

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

`docs/` contains implementation guides, status reports, and reference materials.

## Document Index

| Document | Purpose |
|----------|---------|
| **`QUICKSTART_GUIDE.md`** | Getting started with BrowserNERD |
| **`IMPLEMENTATION_STATUS.md`** | Feature completion status |
| **`VALIDATION_REPORT.md`** | Test validation results |
| **`COMPLETION_SUMMARY.md`** | Development summary |
| **`agents.md`** | Agent predicate schemas |

## Quick Reference

### Starting the Server

```bash
# Stdio mode (Claude Desktop)
./bin/browsernerd --config config.yaml

# SSE mode (HTTP)
./bin/browsernerd --config config.yaml --sse-port 8080
```

### Basic Workflow

1. `launch-browser` - Start Chrome
2. `create-session` - Open new page
3. `navigate-url` - Go to URL
4. `get-interactive-elements` - Find elements
5. `interact` - Click/type
6. `get-console-errors` - Check for errors

### Testing with Mangle

1. `submit-rule` - Define test assertion
2. Perform actions
3. `evaluate-rule` - Check if passed

### Token-Efficient Automation (NEW)

Use these tools to reduce context usage and improve reliability:

1. **`await-stable-state`**: Call this **BEFORE** taking screenshots or extracting data.
   - Blocks until network is idle (500ms) and DOM is settled (200ms).
   - Prevents "flaky" reads on loading pages.

2. **`diagnose-page`**: Call this **AFTER** a failure or unexpected state.
   - Returns a concise root cause analysis (e.g., "API 500 caused JS Error").
   - **DO NOT** read raw console logs unless this tool fails to find the issue.

Example:

```json
// 1. Wait for page to settle
{"tool": "await-stable-state", "arguments": {"timeout_ms": 5000}}

// 2. Check for errors if something looks wrong
{"tool": "diagnose-page", "arguments": {}}
```

// Navigate
{"tool": "navigate-url", "arguments": {"session_id": "sess-1", "url": "<http://localhost:3000/login"}}>

// Fill form
{"tool": "fill-form", "arguments": {"session_id": "sess-1", "fields": [{"ref": "email", "value": "user@example.com"}], "submit": true}}

// Check result
{"tool": "evaluate-rule", "arguments": {"predicate": "test_passed"}}

```

## Implementation Status

See `IMPLEMENTATION_STATUS.md` for detailed feature completion:

- Session management
- Page interaction
- React Fiber extraction
- CDP event ingestion
- Mangle integration
- Causal reasoning

## Validation

See `VALIDATION_REPORT.md` for test results covering:

- Unit tests
- Integration tests
- MCP protocol compliance
- Error handling

## Related Documentation

- [../CLAUDE.md](../CLAUDE.md) - Server root documentation
- [../schemas/CLAUDE.md](../schemas/CLAUDE.md) - Mangle schema reference
- [../internal/CLAUDE.md](../internal/CLAUDE.md) - Implementation details
