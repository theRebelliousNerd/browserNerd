#!/usr/bin/env python3
"""
BrowserNERD MCP smoke-test harness.

Why this exists:
- BrowserNERD is a stdio MCP server (newline-delimited JSON-RPC over stdin/stdout).
- It must be kept alive; if stdin is closed it will exit (EOF).
- This harness lets us test a freshly-built browsernerd.exe without needing Codex/MCP reload cycles.
"""

from __future__ import annotations

import argparse
import json
import os
import queue
import subprocess
import sys
import threading
import time
from dataclasses import dataclass
from typing import Any


MCP_PROTOCOL_VERSION = "2025-06-18"


@dataclass(frozen=True)
class RpcError(Exception):
    error: dict[str, Any]

    def __str__(self) -> str:
        return f"RPC error: {json.dumps(self.error, ensure_ascii=True)}"


class McpStdioClient:
    def __init__(self, exe_path: str, exe_args: list[str], *, debug: bool = False) -> None:
        self._debug = debug
        self._next_id = 1
        self._resp_by_id: dict[int, "queue.Queue[dict[str, Any]]"] = {}
        self._resp_lock = threading.Lock()

        # Keep text mode to simplify newline framing. bufsize=1 enables line buffering.
        self._proc = subprocess.Popen(
            [exe_path, *exe_args],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            bufsize=1,
        )
        if self._proc.stdin is None or self._proc.stdout is None or self._proc.stderr is None:
            raise RuntimeError("Failed to open stdio pipes for MCP server process")

        self._stdin = self._proc.stdin
        self._stdout = self._proc.stdout
        self._stderr = self._proc.stderr

        self._stderr_lines: "queue.Queue[str]" = queue.Queue()
        self._stdout_reader = threading.Thread(target=self._read_stdout, name="mcp-stdout", daemon=True)
        self._stderr_reader = threading.Thread(target=self._read_stderr, name="mcp-stderr", daemon=True)
        self._stdout_reader.start()
        self._stderr_reader.start()

    def _log_debug(self, msg: str) -> None:
        if self._debug:
            print(f"[mcp_smoke] {msg}", file=sys.stderr)

    def _read_stdout(self) -> None:
        while True:
            line = self._stdout.readline()
            if line == "":
                self._log_debug("stdout closed")
                return
            line = line.strip()
            if not line:
                continue
            try:
                msg = json.loads(line)
            except Exception:
                self._log_debug(f"non-json stdout line: {line[:200]}")
                continue

            msg_id = msg.get("id")
            if msg_id is None:
                # Notifications: keep them visible when debugging.
                self._log_debug(f"notification: {json.dumps(msg, ensure_ascii=True)[:500]}")
                continue

            if not isinstance(msg_id, int):
                self._log_debug(f"unexpected non-int id: {msg_id!r}")
                continue

            with self._resp_lock:
                q = self._resp_by_id.get(msg_id)
            if q is None:
                self._log_debug(f"response for unknown id {msg_id}: {json.dumps(msg, ensure_ascii=True)[:500]}")
                continue
            q.put(msg)

    def _read_stderr(self) -> None:
        while True:
            line = self._stderr.readline()
            if line == "":
                return
            self._stderr_lines.put(line.rstrip("\n"))

    def _ensure_running(self) -> None:
        rc = self._proc.poll()
        if rc is not None:
            # Drain any stderr to make failures actionable.
            stderr_tail: list[str] = []
            while not self._stderr_lines.empty() and len(stderr_tail) < 50:
                try:
                    stderr_tail.append(self._stderr_lines.get_nowait())
                except queue.Empty:
                    break
            tail = "\n".join(stderr_tail)
            raise RuntimeError(f"MCP server exited early (code {rc}). Stderr tail:\n{tail}")

    def notify(self, method: str, params: dict[str, Any] | None = None) -> None:
        self._ensure_running()
        msg: dict[str, Any] = {"jsonrpc": "2.0", "method": method}
        if params is not None:
            msg["params"] = params
        payload = json.dumps(msg, separators=(",", ":"), ensure_ascii=True)
        self._log_debug(f"--> notify {method}")
        self._stdin.write(payload + "\n")
        self._stdin.flush()

    def request(self, method: str, params: dict[str, Any] | None = None, *, timeout_s: float = 15.0) -> Any:
        self._ensure_running()
        req_id = self._next_id
        self._next_id += 1

        q: "queue.Queue[dict[str, Any]]" = queue.Queue(maxsize=1)
        with self._resp_lock:
            self._resp_by_id[req_id] = q

        try:
            msg: dict[str, Any] = {"jsonrpc": "2.0", "id": req_id, "method": method}
            if params is not None:
                msg["params"] = params
            payload = json.dumps(msg, separators=(",", ":"), ensure_ascii=True)
            self._log_debug(f"--> req {req_id} {method}")
            self._stdin.write(payload + "\n")
            self._stdin.flush()

            try:
                resp = q.get(timeout=timeout_s)
            except queue.Empty:
                self._ensure_running()
                raise TimeoutError(f"Timed out waiting for response to {method} (id={req_id})")

            if "error" in resp and resp["error"] is not None:
                raise RpcError(resp["error"])
            return resp.get("result")
        finally:
            with self._resp_lock:
                self._resp_by_id.pop(req_id, None)

    def close(self) -> None:
        try:
            if self._proc.poll() is None:
                self._proc.terminate()
                try:
                    self._proc.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    self._proc.kill()
        finally:
            # Best-effort cleanup; Python will close pipes on exit.
            pass


def _default_exe_path() -> str:
    here = os.path.abspath(os.path.dirname(__file__))
    return os.path.abspath(os.path.join(here, "..", "browsernerd.exe"))


def _default_config_path() -> str:
    here = os.path.abspath(os.path.dirname(__file__))
    return os.path.abspath(os.path.join(here, "..", "config.yaml"))


def _build_exe_args(config_path: str) -> list[str]:
    return ["--config", config_path]


def _init(client: McpStdioClient) -> dict[str, Any]:
    result = client.request(
        "initialize",
        params={
            "protocolVersion": MCP_PROTOCOL_VERSION,
            "capabilities": {},
            "clientInfo": {"name": "browsernerd-smoke", "version": "0.1"},
        },
        timeout_s=15.0,
    )
    client.notify("notifications/initialized", params={})
    return result


def _tools_list(client: McpStdioClient) -> list[dict[str, Any]]:
    result = client.request("tools/list", params={}, timeout_s=15.0)
    tools = result.get("tools", [])
    if not isinstance(tools, list):
        raise RuntimeError(f"Unexpected tools/list result: {result!r}")
    return tools


def _resources_list(client: McpStdioClient) -> list[dict[str, Any]]:
    result = client.request("resources/list", params={}, timeout_s=15.0)
    resources = result.get("resources", [])
    if not isinstance(resources, list):
        raise RuntimeError(f"Unexpected resources/list result: {result!r}")
    return resources


def _resource_templates_list(client: McpStdioClient) -> list[dict[str, Any]]:
    result = client.request("resources/templates/list", params={}, timeout_s=15.0)
    templates = result.get("resourceTemplates", [])
    if not isinstance(templates, list):
        raise RuntimeError(f"Unexpected resources/templates/list result: {result!r}")
    return templates


def _tool_call(client: McpStdioClient, name: str, arguments: dict[str, Any] | None = None, *, timeout_s: float = 60.0) -> Any:
    if arguments is None:
        arguments = {}
    raw = client.request("tools/call", params={"name": name, "arguments": arguments}, timeout_s=timeout_s)

    # MCP tools/call result is typically {"content":[...], "isError": bool?}.
    # BrowserNERD returns most structured payloads as a single text content item containing JSON.
    if not isinstance(raw, dict):
        return raw
    if raw.get("isError"):
        return raw
    content = raw.get("content")
    if not isinstance(content, list) or not content:
        return raw

    parsed_items: list[Any] = []
    for item in content:
        if not isinstance(item, dict):
            parsed_items.append(item)
            continue
        if item.get("type") == "text" and isinstance(item.get("text"), str):
            text = item["text"].strip()
            try:
                parsed_items.append(json.loads(text))
            except Exception:
                parsed_items.append(item["text"])
            continue
        parsed_items.append(item)

    if len(parsed_items) == 1:
        return parsed_items[0]
    return parsed_items


def _cmd_list(args: argparse.Namespace) -> int:
    client = McpStdioClient(args.exe, _build_exe_args(args.config), debug=args.debug)
    try:
        init = _init(client)
        server_info = init.get("serverInfo", {})
        print(f"Connected. Server: {server_info.get('name', '?')} {server_info.get('version', '?')}")

        tools = _tools_list(client)
        print(f"Tools ({len(tools)}): " + ", ".join(t.get("name", "?") for t in tools))

        resources = _resources_list(client)
        print(f"Resources ({len(resources)}): " + ", ".join(r.get("uri", "?") for r in resources))

        templates = _resource_templates_list(client)
        print(f"Resource templates ({len(templates)}): " + ", ".join(t.get("uriTemplate", "?") for t in templates))
        return 0
    finally:
        client.close()


def _cmd_call(args: argparse.Namespace) -> int:
    client = McpStdioClient(args.exe, _build_exe_args(args.config), debug=args.debug)
    try:
        _init(client)

        tool_args: dict[str, Any] = {}
        if args.arguments_json:
            tool_args = json.loads(args.arguments_json)
            if not isinstance(tool_args, dict):
                raise ValueError("--arguments-json must parse to a JSON object")

        result = _tool_call(client, args.name, tool_args, timeout_s=args.timeout_s)
        # Print as compact JSON for easy copy/paste.
        print(json.dumps(result, indent=2, ensure_ascii=True))
        return 0
    finally:
        client.close()


def _cmd_smoke(args: argparse.Namespace) -> int:
    client = McpStdioClient(args.exe, _build_exe_args(args.config), debug=args.debug)
    session_id: str | None = None
    try:
        _init(client)
        _tool_call(client, "launch-browser", {}, timeout_s=60.0)

        if args.url:
            created = _tool_call(client, "create-session", {"url": args.url}, timeout_s=60.0)
        else:
            created = _tool_call(client, "create-session", {}, timeout_s=60.0)

        # create-session returns: { session: {id,url,title} }
        session = created.get("session", {}) if isinstance(created, dict) else {}
        session_id = session.get("id")
        if not isinstance(session_id, str) or not session_id:
            raise RuntimeError(f"Unexpected create-session result: {created!r}")

        if args.url:
            _tool_call(
                client,
                "navigate-url",
                {"session_id": session_id, "url": args.url, "wait_until": "networkidle"},
                timeout_s=120.0,
            )

        _tool_call(client, "await-stable-state", {"session_id": session_id}, timeout_s=30.0)
        state = _tool_call(client, "get-page-state", {"session_id": session_id}, timeout_s=30.0)
        print(f"Page: {state.get('title', '?')} ({state.get('url', '?')})  session_id={session_id}")

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
        if isinstance(observe, dict):
            print(observe.get("summary", "observe complete"))
            next_step = observe.get("next_step", {})
            if isinstance(next_step, dict) and next_step:
                print(f"Next step: {next_step.get('tool', '?')}  reason={next_step.get('reason', '')}")

            data = observe.get("data", {})
            if isinstance(data, dict):
                nav = data.get("nav", {})
                if isinstance(nav, dict):
                    counts = nav.get("counts", {})
                    if isinstance(counts, dict):
                        print(f"Nav links: total={counts.get('total', '?')} internal={counts.get('internal', '?')} external={counts.get('external', '?')}")

                inter = data.get("interactive", {})
                if isinstance(inter, dict):
                    summary = inter.get("summary", {})
                    if isinstance(summary, dict):
                        print(f"Interactive: total={summary.get('total', '?')} types={summary.get('types', {})}")

                diag = data.get("diagnostics", {})
                if isinstance(diag, dict) and diag.get("status"):
                    print(f"Diagnose: status={diag.get('status', '?')} summary={diag.get('summary', '')}")
        else:
            print("Observe returned non-object result")
        return 0
    finally:
        try:
            if session_id:
                _tool_call(client, "shutdown-browser", {}, timeout_s=60.0)
        except Exception as e:
            print(f"Warning: failed to shutdown-browser cleanly: {e}", file=sys.stderr)
        client.close()


def _cmd_repl(args: argparse.Namespace) -> int:
    import shlex

    client = McpStdioClient(args.exe, _build_exe_args(args.config), debug=args.debug)
    try:
        init = _init(client)
        server_info = init.get("serverInfo", {})
        print(f"Connected. Server: {server_info.get('name', '?')} {server_info.get('version', '?')}")
        print("Commands: tools | resources | templates | call <tool> [<json-args>] | quit")

        cached_tools: list[dict[str, Any]] | None = None

        while True:
            try:
                line = input("mcp> ").strip()
            except (EOFError, KeyboardInterrupt):
                print()
                break

            if not line:
                continue

            if line in {"quit", "exit"}:
                break

            if line in {"tools", "t"}:
                cached_tools = _tools_list(client)
                print(", ".join(t.get("name", "?") for t in cached_tools))
                continue

            if line in {"resources", "r"}:
                resources = _resources_list(client)
                print(", ".join(r.get("uri", "?") for r in resources))
                continue

            if line in {"templates"}:
                templates = _resource_templates_list(client)
                print(", ".join(t.get("uriTemplate", "?") for t in templates))
                continue

            if line in {"help", "h", "?"}:
                print("Commands: tools | resources | templates | call <tool> [<json-args>] | quit")
                continue

            if line.startswith("call "):
                parts = shlex.split(line)
                if len(parts) < 2:
                    print("Usage: call <tool> [<json-args>]")
                    continue
                tool_name = parts[1]
                raw_args = parts[2] if len(parts) >= 3 else "{}"
                tool_args: dict[str, Any] = {}
                if raw_args:
                    parsed = json.loads(raw_args)
                    if not isinstance(parsed, dict):
                        raise ValueError("tool args must be a JSON object")
                    tool_args = parsed
                result = _tool_call(client, tool_name, tool_args, timeout_s=args.timeout_s)
                print(json.dumps(result, indent=2, ensure_ascii=True))
                continue

            # Convenience: allow typing a tool name directly.
            if cached_tools is None:
                cached_tools = _tools_list(client)
            known = {t.get("name", "") for t in cached_tools}
            if line in known:
                result = _tool_call(client, line, {}, timeout_s=args.timeout_s)
                print(json.dumps(result, indent=2, ensure_ascii=True))
                continue

            print("Unknown command. Type 'help'.")

        return 0
    finally:
        client.close()


def main() -> int:
    parser = argparse.ArgumentParser(description="BrowserNERD MCP stdio harness")
    parser.add_argument("--exe", default=_default_exe_path(), help="Path to browsernerd.exe")
    parser.add_argument("--config", default=_default_config_path(), help="Path to BrowserNERD MCP config.yaml")
    parser.add_argument("--debug", action="store_true", help="Verbose debug logs (stderr)")

    sub = parser.add_subparsers(dest="cmd", required=True)

    p_list = sub.add_parser("list", help="Initialize then list tools/resources/templates")
    p_list.set_defaults(func=_cmd_list)

    p_call = sub.add_parser("call", help="Call a specific MCP tool")
    p_call.add_argument("--name", required=True, help="Tool name (e.g. list-sessions, create-session)")
    p_call.add_argument("--arguments-json", default="", help='Tool args as JSON object (e.g. {"url":"https://example.com"})')
    p_call.add_argument("--timeout-s", type=float, default=60.0, help="Tool call timeout seconds")
    p_call.set_defaults(func=_cmd_call)

    p_smoke = sub.add_parser("smoke", help="Basic end-to-end browser flow (launch, create session, inspect)")
    p_smoke.add_argument("--url", default="https://symbiogen.ai/", help="URL to navigate to")
    p_smoke.set_defaults(func=_cmd_smoke)

    p_repl = sub.add_parser("repl", help="Interactive REPL for ad-hoc MCP tool calls (keeps one server alive)")
    p_repl.add_argument("--timeout-s", type=float, default=60.0, help="Default timeout seconds for tool calls")
    p_repl.set_defaults(func=_cmd_repl)

    args = parser.parse_args()
    return int(args.func(args))


if __name__ == "__main__":
    raise SystemExit(main())
