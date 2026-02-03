"""Condition 3: HTML + Screenshot baseline — Playwright fetches HTML and viewport screenshot."""

from __future__ import annotations

import base64
import logging
import time

import anthropic
from playwright.async_api import async_playwright

from config import HarnessSettings, TaskDef, TaskDefaults
from metrics import TaskMetrics, TurnMetrics

from .base import BaseRunner

logger = logging.getLogger(__name__)

SYSTEM_PROMPT = """\
You are an information extraction agent. You will be given the HTML of a web page along \
with a screenshot of the page. Use both to extract the requested information and return it \
as your answer."""


class HTMLScreenshotRunner(BaseRunner):
    def __init__(self, settings: HarnessSettings, defaults: TaskDefaults) -> None:
        super().__init__(settings, defaults)
        self._api = anthropic.Anthropic()
        self._playwright = None
        self._browser = None

    @property
    def condition_name(self) -> str:
        return "html_screenshot"

    async def setup(self) -> None:
        self._playwright = await async_playwright().start()
        self._browser = await self._playwright.chromium.launch(headless=True)

    async def run_task(self, task: TaskDef, run: int) -> TaskMetrics:
        metrics = TaskMetrics(task_id=task.id, condition=self.condition_name, run=run)

        # Fetch page with Playwright
        page = await self._browser.new_page(viewport={"width": 1920, "height": 1080})
        try:
            await page.goto(task.url, wait_until="networkidle", timeout=30_000)
            html = await page.content()
            screenshot_bytes = await page.screenshot(type="png", full_page=False)
        finally:
            await page.close()

        # Truncate large HTML
        max_chars = 500_000
        if len(html) > max_chars:
            html = html[:max_chars] + "\n<!-- truncated -->"
            logger.warning("Truncated HTML for %s", task.url)

        screenshot_b64 = base64.standard_b64encode(screenshot_bytes).decode("ascii")

        user_content = [
            {
                "type": "text",
                "text": (
                    f"Here is the HTML of the page at {task.url}:\n\n"
                    f"```html\n{html}\n```\n\n"
                    f"And here is a screenshot of the page:"
                ),
            },
            {
                "type": "image",
                "source": {
                    "type": "base64",
                    "media_type": "image/png",
                    "data": screenshot_b64,
                },
            },
            {
                "type": "text",
                "text": task.prompt,
            },
        ]

        t0 = time.monotonic()
        response = self._api.messages.create(
            model=self._effective_model(),
            max_tokens=4096,
            system=SYSTEM_PROMPT,
            messages=[{"role": "user", "content": user_content}],
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
        if self._browser:
            await self._browser.close()
        if self._playwright:
            await self._playwright.stop()
