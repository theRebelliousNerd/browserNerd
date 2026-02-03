"""Condition 1: BrowserNERD MCP runner."""

from __future__ import annotations

import logging

import anthropic

from config import HarnessSettings, TaskDef, TaskDefaults
from conversation import run_mcp_conversation
from mcp_client import MCPClient
from metrics import TaskMetrics

from .base import BaseRunner

logger = logging.getLogger(__name__)

SYSTEM_PROMPT = """\
You are an information extraction agent using the BrowserNERD browser automation toolkit.

To extract information from a web page, follow these steps:
1. Call `launch-browser` to ensure a browser is running.
2. Call `create-session` to open a new tab.
3. Call `navigate-url` with the target URL.
4. Use extraction tools (get-page-state, snapshot-dom, evaluate-js, screenshot, \
get-interactive-elements, get-navigation-links) to gather the requested information.
5. Return the extracted information as your final text answer.

Be efficient with tool calls. Prefer `evaluate-js` for targeted extraction when you \
know the DOM structure, and `snapshot-dom` when you need to explore the page."""


class BrowserNERDMCPRunner(BaseRunner):
    def __init__(self, settings: HarnessSettings, defaults: TaskDefaults) -> None:
        super().__init__(settings, defaults)
        self._api = anthropic.Anthropic()

    @property
    def condition_name(self) -> str:
        return "browsernerd_mcp"

    async def setup(self) -> None:
        # Nothing global — MCP subprocess is created fresh per task
        assert self.settings.browsernerd_binary is not None, (
            "--browsernerd-binary is required for browsernerd_mcp condition"
        )

    async def run_task(self, task: TaskDef, run: int) -> TaskMetrics:
        metrics = TaskMetrics(task_id=task.id, condition=self.condition_name, run=run)

        args = ["--mode", "stdio"]
        if self.settings.browsernerd_config:
            args.extend(["--config", str(self.settings.browsernerd_config)])

        mcp = MCPClient(command=str(self.settings.browsernerd_binary), args=args)
        try:
            await mcp.start()
            answer = await run_mcp_conversation(
                client=self._api,
                mcp=mcp,
                model=self._effective_model(),
                system_prompt=SYSTEM_PROMPT,
                user_message=f"Navigate to {task.url} and answer: {task.prompt}",
                max_turns=self._effective_max_turns(),
                task_metrics=metrics,
            )
            metrics.final_answer = answer
        finally:
            await mcp.stop()

        metrics.finalize()
        return metrics

    async def teardown(self) -> None:
        pass
