"""Condition 6: Chrome DevTools MCP runner - Chrome DevTools Protocol based MCP server (Gemini version)."""

from __future__ import annotations

import logging
import os

from google import genai

from config import HarnessSettings, TaskDef, TaskDefaults
from conversation import run_mcp_conversation
from mcp_client import MCPClient
from metrics import TaskMetrics

from .base import BaseRunner

logger = logging.getLogger(__name__)

SYSTEM_PROMPT = """\
You are an information extraction agent using Chrome DevTools Protocol browser automation tools. \
Use the available tools to navigate to the target page, interact with it as needed, \
and extract the requested information. Return the extracted information as your final \
text answer."""


class ChromeDevToolsMCPRunner(BaseRunner):
    def __init__(self, settings: HarnessSettings, defaults: TaskDefaults) -> None:
        super().__init__(settings, defaults)
        api_key = os.environ.get("GEMINI_API_KEY")
        if not api_key:
            raise ValueError("GEMINI_API_KEY environment variable is required")
        self._client = genai.Client(api_key=api_key)

    @property
    def condition_name(self) -> str:
        return "chrome_devtools_mcp"

    async def setup(self) -> None:
        assert self.settings.chrome_devtools_command is not None, (
            "--chrome-devtools-command is required for chrome_devtools_mcp condition"
        )

    async def run_task(self, task: TaskDef, run: int) -> TaskMetrics:
        metrics = TaskMetrics(task_id=task.id, condition=self.condition_name, run=run)

        mcp = MCPClient(
            command=self.settings.chrome_devtools_command,
            args=self.settings.chrome_devtools_args,
        )
        try:
            await mcp.start()
            answer = await run_mcp_conversation(
                client=self._client,
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
