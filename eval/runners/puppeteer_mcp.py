"""Condition 4: Puppeteer MCP runner — same conversation loop, different MCP server."""

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
You are an information extraction agent using browser automation tools. \
Use the available tools to navigate to the target page, interact with it as needed, \
and extract the requested information. Return the extracted information as your final \
text answer."""


class PuppeteerMCPRunner(BaseRunner):
    def __init__(self, settings: HarnessSettings, defaults: TaskDefaults) -> None:
        super().__init__(settings, defaults)
        self._api = anthropic.Anthropic()

    @property
    def condition_name(self) -> str:
        return "puppeteer_mcp"

    async def setup(self) -> None:
        assert self.settings.puppeteer_command is not None, (
            "--puppeteer-command is required for puppeteer_mcp condition"
        )

    async def run_task(self, task: TaskDef, run: int) -> TaskMetrics:
        metrics = TaskMetrics(task_id=task.id, condition=self.condition_name, run=run)

        mcp = MCPClient(
            command=self.settings.puppeteer_command,
            args=self.settings.puppeteer_args,
        )
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
