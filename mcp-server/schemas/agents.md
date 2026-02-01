# agents.md

Mangle predicate definitions and rules for BrowserNERD.

- `browser.mg` encodes React (`react_component/prop/state`, `dom_mapping`), DOM (`dom_node/attr`), network (`net_request/response/header`), console/navigation, and derived RCA/test predicates, matching the PRD vectors.
- Derived rules capture causal chains (`caused_by`, `cascading_failure`), performance (`slow_api`), race detection, and declarative pass conditions (`test_passed`).
- When extending the schema, keep predicate names stable with emitters in `../internal/browser` and tools in `../internal/mcp`; add rules sparingly to avoid solver overhead.
- Cross-check new predicates against `../docs/mangle-programming-references` and PRD sections on the Flight Recorder and logic-based test runner.
