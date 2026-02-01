# agents.md

Wrapper around google/mangle for BrowserNERD.

- Loads `../../schemas/browser.mg`, manages the fact buffer and predicate index, and runs incremental evaluation via `engine.EvalProgram`.
- Supports `AddRule` and `Query` so agents can submit logic-based assertions and time-travel debugging checks from the PRD’s Vector 4.
- Fact ingestion uses a circular buffer respecting `FactBufferLimit`; new facts must be schema-aligned and timestamped.
- Consult `../../docs/mangle-programming-references` for syntax/examples before extending predicates or arithmetic helpers.
