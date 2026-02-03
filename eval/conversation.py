"""Multi-turn tool_use conversation loop shared by MCP runners."""

from __future__ import annotations

import logging
import time
from typing import Any

import anthropic

from mcp_client import MCPClient
from metrics import TaskMetrics, TurnMetrics

logger = logging.getLogger(__name__)


async def run_mcp_conversation(
    *,
    client: anthropic.Anthropic,
    mcp: MCPClient,
    model: str,
    system_prompt: str,
    user_message: str,
    max_turns: int,
    task_metrics: TaskMetrics,
) -> str:
    """Drive a multi-turn conversation with tool use via MCP.

    Returns the final assistant text answer.
    """
    tools = await mcp.list_tools()
    messages: list[dict[str, Any]] = [{"role": "user", "content": user_message}]

    for turn_idx in range(max_turns):
        t0 = time.monotonic()
        response = client.messages.create(
            model=model,
            max_tokens=4096,
            system=system_prompt,
            tools=tools,
            messages=messages,
        )
        elapsed = time.monotonic() - t0

        # Count tool_use blocks in the response
        tool_use_blocks = [b for b in response.content if b.type == "tool_use"]

        turn = TurnMetrics(
            turn=turn_idx,
            input_tokens=response.usage.input_tokens,
            output_tokens=response.usage.output_tokens,
            tool_calls=len(tool_use_blocks),
            wall_clock_s=elapsed,
        )
        task_metrics.turns.append(turn)
        logger.debug(
            "Turn %d: %d input, %d output tokens, %d tool calls, %.1fs",
            turn_idx,
            turn.input_tokens,
            turn.output_tokens,
            turn.tool_calls,
            turn.wall_clock_s,
        )

        # If the model stopped without requesting tools, extract final answer
        if response.stop_reason != "tool_use":
            text_parts = [b.text for b in response.content if hasattr(b, "text")]
            return "\n".join(text_parts)

        # Append assistant message (preserves tool_use blocks)
        messages.append({"role": "assistant", "content": response.content})

        # Forward each tool call to MCP and collect results
        tool_results: list[dict[str, Any]] = []
        for block in tool_use_blocks:
            logger.debug("Calling tool %s(%s)", block.name, block.input)
            try:
                result_text = await mcp.call_tool(block.name, block.input)
                tool_results.append({
                    "type": "tool_result",
                    "tool_use_id": block.id,
                    "content": result_text,
                })
            except Exception:
                logger.exception("Tool %s failed", block.name)
                tool_results.append({
                    "type": "tool_result",
                    "tool_use_id": block.id,
                    "content": f"Error: tool {block.name} failed",
                    "is_error": True,
                })

        messages.append({"role": "user", "content": tool_results})

    # Exhausted max_turns — ask for a final answer without tools
    messages.append({
        "role": "user",
        "content": "You have reached the maximum number of turns. Please provide your final answer now.",
    })
    t0 = time.monotonic()
    response = client.messages.create(
        model=model,
        max_tokens=4096,
        system=system_prompt,
        messages=messages,
    )
    elapsed = time.monotonic() - t0
    task_metrics.turns.append(TurnMetrics(
        turn=max_turns,
        input_tokens=response.usage.input_tokens,
        output_tokens=response.usage.output_tokens,
        tool_calls=0,
        wall_clock_s=elapsed,
    ))
    text_parts = [b.text for b in response.content if hasattr(b, "text")]
    return "\n".join(text_parts)
