#!/usr/bin/env python3
"""
SymbioGen cloud login + dashboard observe smoke test for BrowserNERD.

Goal:
- Validate BrowserNERD can drive the SymbioGen Cloudflare tunnel site.
- Specifically, validate browser-observe action planning is scoped to the *current* page
  (no stale login-page action candidates after reaching /dashboard).

Credentials:
- SYMBIOGEN_EMAIL (default: steve@nextgenrd.com)
- SYMBIOGEN_PASSWORD (required)
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
from typing import Any

from mcp_smoke import McpStdioClient, _build_exe_args, _default_config_path, _default_exe_path, _init, _tool_call


def _norm(s: str) -> str:
    return " ".join(s.strip().lower().split())


def _extract_elements(payload: Any) -> list[dict[str, Any]]:
    if not isinstance(payload, dict):
        return []
    raw = payload.get("elements", [])
    if not isinstance(raw, list):
        return []
    out: list[dict[str, Any]] = []
    for item in raw:
        if isinstance(item, dict):
            out.append(item)
    return out


def _find_ref(
    elements: list[dict[str, Any]],
    *,
    label_contains: list[str] | None = None,
    ref_contains: list[str] | None = None,
    allowed_types: set[str] | None = None,
) -> str | None:
    label_contains = label_contains or []
    ref_contains = ref_contains or []
    label_needles = [_norm(s) for s in label_contains if _norm(s)]
    ref_needles = [_norm(s) for s in ref_contains if _norm(s)]

    for el in elements:
        ref = str(el.get("ref", "")).strip()
        if not ref:
            continue
        typ = _norm(str(el.get("type", "")))
        if allowed_types is not None and typ not in allowed_types:
            continue

        label = _norm(str(el.get("label", "")))
        ref_norm = _norm(ref)

        ok_label = True
        if label_needles:
            ok_label = any(n in label for n in label_needles)

        ok_ref = True
        if ref_needles:
            ok_ref = any(n in ref_norm for n in ref_needles)

        if ok_label and ok_ref:
            return ref
    return None


def _wait_for_url_contains(client: McpStdioClient, session_id: str, needle: str, *, timeout_s: float) -> str:
    deadline = time.time() + timeout_s
    last_url = ""
    while time.time() < deadline:
        state = _tool_call(client, "get-page-state", {"session_id": session_id}, timeout_s=30.0)
        if isinstance(state, dict):
            last_url = str(state.get("url", "")).strip()
            if needle in last_url:
                return last_url
        time.sleep(0.75)
    raise TimeoutError(f"Timed out waiting for url to contain {needle!r}. Last url={last_url!r}")


def _has_ref(candidate: Any, ref: str) -> bool:
    if not isinstance(candidate, dict):
        return False
    return str(candidate.get("ref", "")).strip() == ref


def main() -> int:
    parser = argparse.ArgumentParser(description="SymbioGen cloud login smoke test (BrowserNERD MCP)")
    parser.add_argument("--exe", default=_default_exe_path(), help="Path to browsernerd.exe")
    parser.add_argument("--config", default=_default_config_path(), help="Path to BrowserNERD MCP config.yaml")
    parser.add_argument("--url", default="https://symbiogen.ai/", help="Base URL (default: https://symbiogen.ai/)")
    parser.add_argument("--dashboard-path", default="/dashboard", help="Dashboard path to assert after login")
    parser.add_argument("--email", default=os.environ.get("SYMBIOGEN_EMAIL", "steve@nextgenrd.com"))
    parser.add_argument("--password-env", default="SYMBIOGEN_PASSWORD", help="Env var name containing password")
    parser.add_argument("--timeout-s", type=float, default=120.0, help="Overall step timeout (seconds)")
    parser.add_argument("--debug", action="store_true", help="Verbose MCP stdout/stderr logs")
    args = parser.parse_args()

    password = os.environ.get(args.password_env, "").strip()
    if not password:
        print(
            f"Missing password env var {args.password_env!r}. Example (PowerShell):\n"
            f'  $env:{args.password_env}=\"...\"; python .\\scripts\\symbiogen_cloud_login_smoke.py\n',
            file=sys.stderr,
        )
        return 2

    client = McpStdioClient(args.exe, _build_exe_args(args.config), debug=args.debug)
    session_id = ""
    login_ref = ""
    email_ref = ""
    password_ref = ""
    try:
        _init(client)

        _tool_call(client, "launch-browser", {}, timeout_s=60.0)
        session = _tool_call(client, "create-session", {"url": args.url}, timeout_s=60.0)
        if not isinstance(session, dict) or not isinstance(session.get("session"), dict):
            raise RuntimeError(f"Unexpected create-session result: {session!r}")
        session_id = str(session["session"].get("id", "")).strip()
        if not session_id:
            raise RuntimeError(f"create-session returned empty session id: {session!r}")

        _tool_call(client, "await-stable-state", {"session_id": session_id}, timeout_s=30.0)

        # 1) Open login modal.
        inter_all = _tool_call(
            client,
            "get-interactive-elements",
            {"session_id": session_id, "filter": "all", "limit": 200, "visible_only": True},
            timeout_s=60.0,
        )
        elements = _extract_elements(inter_all)
        login_ref = _find_ref(elements, label_contains=["Client Login", "Login", "Sign In"], allowed_types=None) or ""
        if not login_ref:
            raise RuntimeError("Could not find a login trigger (looked for label contains: Client Login/Login/Sign In)")

        _tool_call(client, "interact", {"session_id": session_id, "action": "click", "ref": login_ref}, timeout_s=60.0)
        _tool_call(client, "await-stable-state", {"session_id": session_id}, timeout_s=30.0)

        # 2) Fill credentials.
        inter_inputs = _tool_call(
            client,
            "get-interactive-elements",
            {"session_id": session_id, "filter": "inputs", "limit": 200, "visible_only": True},
            timeout_s=60.0,
        )
        inputs = _extract_elements(inter_inputs)
        email_ref = _find_ref(inputs, label_contains=["email"], ref_contains=["email"], allowed_types={"input"}) or ""
        if not email_ref:
            # Some inputs have no label; fallback to any ref containing email.
            email_ref = _find_ref(inputs, ref_contains=["email"], allowed_types={"input"}) or ""
        if not email_ref:
            raise RuntimeError("Could not find email input (looked for label/ref contains 'email')")

        password_ref = _find_ref(inputs, label_contains=["password"], ref_contains=["password"], allowed_types={"input"}) or ""
        if not password_ref:
            password_ref = _find_ref(inputs, ref_contains=["password"], allowed_types={"input"}) or ""
        if not password_ref:
            raise RuntimeError("Could not find password input (looked for label/ref contains 'password')")

        _tool_call(
            client,
            "interact",
            {"session_id": session_id, "action": "type", "ref": email_ref, "value": args.email},
            timeout_s=60.0,
        )
        # Submit via Enter after password typing (more robust than searching for the submit button).
        _tool_call(
            client,
            "interact",
            {"session_id": session_id, "action": "type", "ref": password_ref, "value": password, "submit": True},
            timeout_s=60.0,
        )

        # 3) Wait for dashboard.
        dashboard_url = _wait_for_url_contains(client, session_id, args.dashboard_path, timeout_s=args.timeout_s)
        print(f"Logged in. URL={dashboard_url}  session_id={session_id}")

        _tool_call(client, "await-stable-state", {"session_id": session_id}, timeout_s=30.0)

        # 4) Observe + validate action candidates are not stale.
        observe = _tool_call(
            client,
            "browser-observe",
            {
                "session_id": session_id,
                "mode": "composite",
                "view": "compact",
                "max_items": 25,
                "include_action_plan": True,
                "include_diagnostics": True,
            },
            timeout_s=60.0,
        )
        if not isinstance(observe, dict):
            raise RuntimeError(f"browser-observe returned unexpected value: {observe!r}")

        next_step = observe.get("next_step", {})
        if isinstance(next_step, dict):
            print(f"Observe summary: {observe.get('summary', '')}")
            print(
                "Next step:",
                json.dumps(next_step, ensure_ascii=True)[:500],
            )

        data = observe.get("data", {})
        cands: list[Any] = []
        if isinstance(data, dict):
            cands_raw = data.get("action_candidates", [])
            if isinstance(cands_raw, list):
                cands = cands_raw

        # Hard checks: action plan should not reference login-page refs once on dashboard.
        stale_refs = [r for r in [login_ref, email_ref, password_ref] if r]
        if isinstance(next_step, dict):
            args_obj = next_step.get("args", {})
            if isinstance(args_obj, dict):
                ref = str(args_obj.get("ref", "")).strip()
                if ref and ref in stale_refs:
                    raise RuntimeError(f"Stale next_step ref detected on dashboard: {ref!r}")

        for r in stale_refs:
            if any(_has_ref(c, r) for c in cands):
                raise RuntimeError(f"Stale action candidate ref detected on dashboard: {r!r}")

        # Print a small sample for human inspection.
        print(f"Action candidates on dashboard: {len(cands)}")
        for c in cands[:10]:
            if isinstance(c, dict):
                print(
                    f"- {c.get('action','?')} ref={c.get('ref','')} label={c.get('label','')} "
                    f"priority={c.get('priority','?')} reason={c.get('reason','')}"
                )
        print("PASS: No stale login refs in action planning after reaching /dashboard")
        return 0
    finally:
        try:
            if session_id:
                _tool_call(client, "shutdown-browser", {}, timeout_s=60.0)
        except Exception as e:
            print(f"Warning: failed to shutdown-browser cleanly: {e}", file=sys.stderr)
        client.close()


if __name__ == "__main__":
    raise SystemExit(main())

