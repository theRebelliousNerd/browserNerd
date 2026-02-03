"""Condition 1: BrowserNERD MCP runner (Gemini version)."""

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
You are an information extraction agent using the BrowserNERD browser automation toolkit.

WORKFLOW:
1. launch-browser -> Ensure browser is running
2. create-session -> Open a new tab
3. navigate-url(wait_until: "networkidle") -> Load the target URL
4. Extract information using the TOKEN COST hierarchy below
5. Return the extracted information as your final text answer

TOKEN COST HIERARCHY (prefer tools higher in this list):
- get-page-state: LOWEST - URL, title, loading state (use FIRST)
- evaluate-js: LOW - targeted DOM queries when you know the selector
- get-navigation-links: LOW - site navigation and links only
- snapshot-dom: MEDIUM - full DOM when you need to explore structure
- get-interactive-elements: MEDIUM - buttons, inputs, forms
- screenshot: HIGH - visual debugging only, NOT for routine checks

ANTI-PATTERNS TO AVOID:
- Taking screenshots to check if page loaded (use get-page-state)
- Using get-interactive-elements when you only need links (use get-navigation-links)
- Multiple individual interact() calls (use execute-plan for sequences)

For information extraction tasks, prefer evaluate-js with targeted selectors when \
you know what to look for, or snapshot-dom when exploring unknown page structure."""


class BrowserNERDMCPRunner(BaseRunner):
    def __init__(self, settings: HarnessSettings, defaults: TaskDefaults) -> None:
        super().__init__(settings, defaults)
        # Initialize Gemini client with API key from environment
        api_key = os.environ.get("GEMINI_API_KEY")
        if not api_key:
            raise ValueError("GEMINI_API_KEY environment variable is required")
        self._client = genai.Client(api_key=api_key)

    @property
    def condition_name(self) -> str:
        return "browsernerd_mcp"

    async def setup(self) -> None:
        # Nothing global - MCP subprocess is created fresh per task
        assert self.settings.browsernerd_binary is not None, (
            "--browsernerd-binary is required for browsernerd_mcp condition"
        )

    async def run_task(self, task: TaskDef, run: int) -> TaskMetrics:
        metrics = TaskMetrics(task_id=task.id, condition=self.condition_name, run=run)

        # BrowserNERD runs in stdio mode by default (no --mode flag needed)
        args = []
        if self.settings.browsernerd_config:
            args.extend(["-config", str(self.settings.browsernerd_config)])

        mcp = MCPClient(command=str(self.settings.browsernerd_binary), args=args)
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
