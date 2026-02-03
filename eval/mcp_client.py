"""MCP stdio client wrapper: subprocess lifecycle and tool schema conversion."""

from __future__ import annotations

import logging
from contextlib import AsyncExitStack
from typing import Any

from mcp import ClientSession, StdioServerParameters
from mcp.client.stdio import stdio_client

logger = logging.getLogger(__name__)


def _mcp_schema_to_anthropic(mcp_tool: Any) -> dict:
    """Convert an MCP tool definition to Anthropic tool format.

    MCP uses ``inputSchema``; Anthropic expects ``input_schema``.
    """
    return {
        "name": mcp_tool.name,
        "description": mcp_tool.description or "",
        "input_schema": mcp_tool.inputSchema,
    }


class MCPClient:
    """Manages an MCP server subprocess and exposes tool call forwarding."""

    def __init__(self, command: str, args: list[str] | None = None, env: dict[str, str] | None = None) -> None:
        self._server_params = StdioServerParameters(
            command=command,
            args=args or [],
            env=env,
        )
        self._exit_stack = AsyncExitStack()
        self._session: ClientSession | None = None
        self._tools_cache: list[dict] | None = None

    async def start(self) -> None:
        """Start the MCP server subprocess and initialize the session."""
        transport = await self._exit_stack.enter_async_context(
            stdio_client(self._server_params)
        )
        read_stream, write_stream = transport
        self._session = await self._exit_stack.enter_async_context(
            ClientSession(read_stream, write_stream)
        )
        await self._session.initialize()
        logger.info("MCP session initialized for %s", self._server_params.command)

    async def stop(self) -> None:
        """Tear down the session and kill the subprocess."""
        await self._exit_stack.aclose()
        self._session = None
        self._tools_cache = None

    async def list_tools(self) -> list[dict]:
        """Return tools in Anthropic API format (cached after first call)."""
        if self._tools_cache is not None:
            return self._tools_cache
        assert self._session is not None, "MCPClient not started"
        result = await self._session.list_tools()
        self._tools_cache = [_mcp_schema_to_anthropic(t) for t in result.tools]
        logger.info("Discovered %d MCP tools", len(self._tools_cache))
        return self._tools_cache

    async def call_tool(self, name: str, arguments: dict) -> str:
        """Forward a tool call to the MCP server and return text content."""
        assert self._session is not None, "MCPClient not started"
        result = await self._session.call_tool(name, arguments)
        # Collect text content from the result
        parts: list[str] = []
        for block in result.content:
            if hasattr(block, "text"):
                parts.append(block.text)
            else:
                parts.append(str(block))
        return "\n".join(parts)
