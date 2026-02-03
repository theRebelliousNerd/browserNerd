"""Condition 2: Raw HTML baseline — fetch with httpx, pass full HTML as context."""

from __future__ import annotations

import logging
import time

import anthropic
import httpx

from config import HarnessSettings, TaskDef, TaskDefaults
from metrics import TaskMetrics, TurnMetrics

from .base import BaseRunner

logger = logging.getLogger(__name__)

SYSTEM_PROMPT = """\
You are an information extraction agent. You will be given the full HTML of a web page. \
Extract the requested information from the HTML and return it as your answer."""


class RawHTMLRunner(BaseRunner):
    def __init__(self, settings: HarnessSettings, defaults: TaskDefaults) -> None:
        super().__init__(settings, defaults)
        self._api = anthropic.Anthropic()

    @property
    def condition_name(self) -> str:
        return "raw_html"

    async def setup(self) -> None:
        pass

    async def run_task(self, task: TaskDef, run: int) -> TaskMetrics:
        metrics = TaskMetrics(task_id=task.id, condition=self.condition_name, run=run)

        # Fetch HTML
        async with httpx.AsyncClient(follow_redirects=True, timeout=30) as http:
            resp = await http.get(task.url)
            resp.raise_for_status()
            html = resp.text

        # Truncate very large pages to avoid hitting context limits
        max_chars = 600_000
        if len(html) > max_chars:
            html = html[:max_chars] + "\n<!-- truncated -->"
            logger.warning("Truncated HTML for %s from %d to %d chars", task.url, len(resp.text), max_chars)

        user_message = (
            f"Here is the full HTML of the page at {task.url}:\n\n"
            f"```html\n{html}\n```\n\n"
            f"{task.prompt}"
        )

        t0 = time.monotonic()
        response = self._api.messages.create(
            model=self._effective_model(),
            max_tokens=4096,
            system=SYSTEM_PROMPT,
            messages=[{"role": "user", "content": user_message}],
        )
        elapsed = time.monotonic() - t0

        metrics.turns.append(TurnMetrics(
            turn=0,
            input_tokens=response.usage.input_tokens,
            output_tokens=response.usage.output_tokens,
            tool_calls=0,
            wall_clock_s=elapsed,
        ))

        text_parts = [b.text for b in response.content if hasattr(b, "text")]
        metrics.final_answer = "\n".join(text_parts)
        metrics.finalize()
        return metrics

    async def teardown(self) -> None:
        pass
