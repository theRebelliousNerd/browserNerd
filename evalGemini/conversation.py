"""Multi-turn tool_use conversation loop shared by MCP runners (Gemini version)."""

from __future__ import annotations

import logging
import time
from typing import Any

from google import genai
from google.genai import types

from mcp_client import MCPClient
from metrics import TaskMetrics, TurnMetrics

logger = logging.getLogger(__name__)


async def run_mcp_conversation(
    *,
    client: genai.Client,
    mcp: MCPClient,
    model: str,
    system_prompt: str,
    user_message: str,
    max_turns: int,
    task_metrics: TaskMetrics,
) -> str:
    """Drive a multi-turn conversation with tool use via MCP using Gemini.

    Returns the final assistant text answer.
    """
    tool_declarations = await mcp.list_tools()
    tool = types.Tool(function_declarations=tool_declarations)

    # Build initial contents
    contents: list[types.Content] = [
        types.Content(role="user", parts=[types.Part.from_text(text=user_message)])
    ]

    # Configuration with tools and system instruction
    config = types.GenerateContentConfig(
        system_instruction=system_prompt,
        tools=[tool],
        automatic_function_calling=types.AutomaticFunctionCallingConfig(disable=True),
    )

    for turn_idx in range(max_turns):
        t0 = time.monotonic()
        response = client.models.generate_content(
            model=model,
            contents=contents,
            config=config,
        )
        elapsed = time.monotonic() - t0

        # Extract usage metadata (handle None values)
        usage = response.usage_metadata
        input_tokens = (usage.prompt_token_count or 0) if usage else 0
        output_tokens = (usage.candidates_token_count or 0) if usage else 0

        # Check for function calls
        function_calls = response.function_calls or []
        tool_names = [fc.name for fc in function_calls]

        turn = TurnMetrics(
            turn=turn_idx,
            input_tokens=input_tokens,
            output_tokens=output_tokens,
            tool_calls=len(function_calls),
            wall_clock_s=elapsed,
            tools_called=tool_names,
        )
        task_metrics.turns.append(turn)
        logger.debug(
            "Turn %d: %d input, %d output tokens, %d tool calls (%s), %.1fs",
            turn_idx,
            turn.input_tokens,
            turn.output_tokens,
            turn.tool_calls,
            ", ".join(tool_names) if tool_names else "none",
            turn.wall_clock_s,
        )

        # If no function calls, extract final text answer
        if not function_calls:
            text_parts = []
            for candidate in response.candidates or []:
                for part in candidate.content.parts or []:
                    if part.text:
                        text_parts.append(part.text)
            return "\n".join(text_parts)

        # Append model response to contents
        if response.candidates:
            contents.append(response.candidates[0].content)

        # Execute each function call via MCP
        function_response_parts: list[types.Part] = []
        for fc in function_calls:
            logger.debug("Calling tool %s(%s)", fc.name, fc.args)
            try:
                result_text = await mcp.call_tool(fc.name, dict(fc.args) if fc.args else {})
                function_response_parts.append(
                    types.Part.from_function_response(
                        name=fc.name,
                        response={"result": result_text},
                    )
                )
            except Exception:
                logger.exception("Tool %s failed", fc.name)
                function_response_parts.append(
                    types.Part.from_function_response(
                        name=fc.name,
                        response={"error": f"Tool {fc.name} failed"},
                    )
                )

        # Add function responses as tool turn (Gemini 3 uses role="tool" for function responses)
        contents.append(types.Content(role="tool", parts=function_response_parts))

    # Exhausted max_turns - ask for final answer without tools
    contents.append(
        types.Content(
            role="user",
            parts=[types.Part.from_text(
                text="You have reached the maximum number of turns. Please provide your final answer now."
            )]
        )
    )

    # Final call without tools
    final_config = types.GenerateContentConfig(
        system_instruction=system_prompt,
    )
    t0 = time.monotonic()
    response = client.models.generate_content(
        model=model,
        contents=contents,
        config=final_config,
    )
    elapsed = time.monotonic() - t0

    usage = response.usage_metadata
    task_metrics.turns.append(TurnMetrics(
        turn=max_turns,
        input_tokens=(usage.prompt_token_count or 0) if usage else 0,
        output_tokens=(usage.candidates_token_count or 0) if usage else 0,
        tool_calls=0,
        wall_clock_s=elapsed,
        tools_called=[],
    ))

    text_parts = []
    for candidate in response.candidates or []:
        for part in candidate.content.parts or []:
            if part.text:
                text_parts.append(part.text)
    return "\n".join(text_parts)
