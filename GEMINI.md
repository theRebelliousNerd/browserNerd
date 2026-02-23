# BrowserNERD MCP Server

> The token-efficient browser automation MCP server built for AI agents.

## Overview
BrowserNERD provides browser automation that is **50-100x more token efficient** than traditional methods by extracting structured, actionable state and performing built-in causal reasoning. It replaces blind HTML dumps with highly structured, intent-driven operations.

## Core Capabilities
1. **Progressive Disclosure**: Only fetch the data you need using `browser-observe` (modes: state, nav, interactive, hidden, composite).
2. **Action Execution**: Control the browser deterministically using `browser-act` with high-level operations (click, type, navigate, await_stable).
3. **Mangle Reasoning**: Diagnose issues using `browser-reason` (topics: why_failed, next_best_action, what_changed_since).
4. **Context Optimization**: Keep your context window clean by relying on BrowserNERD's compact views.

## When to Use BrowserNERD Tools
- **Need to check the page?** Use `browser-observe` with `intent: "quick_status"`.
- **Need to find a button or link?** Use `browser-observe` with `mode: "interactive"`.
- **Need to click or type?** Use `browser-act` with the element's `ref`.
- **Why did a test/click fail?** Use `browser-reason` with `topic: "why_failed"`.
- **Need the full React tree?** Use `browser-observe` with `mode: "react"`.

## Debugging Workflow
1. Use `launch-browser` and create a session.
2. Observe with `browser-observe`.
3. Interact using `browser-act`.
4. If something fails, immediately call `browser-reason` with `topic="why_failed"` to correlate console, network, and React errors automatically.

## Rules
- **NEVER** request full raw HTML unless explicitly asked by the user. BrowserNERD provides better, structured data via `browser-observe`.
- **ALWAYS** use `refs` returned by `browser-observe` when using `browser-act`.
- Be highly concise when reporting back browser state to the user.
