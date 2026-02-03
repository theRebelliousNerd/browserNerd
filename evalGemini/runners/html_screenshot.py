"""Condition 3: HTML + Screenshot baseline - Playwright fetches HTML and viewport screenshot (Gemini version)."""

from __future__ import annotations

import logging
import os
import time

from google import genai
from google.genai import types
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
        api_key = os.environ.get("GEMINI_API_KEY")
        if not api_key:
            raise ValueError("GEMINI_API_KEY environment variable is required")
        self._client = genai.Client(api_key=api_key)
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

        # Build multimodal content for Gemini 3
        # Use types.Part with inline_data for images
        contents = [
            types.Content(
                role="user",
                parts=[
                    types.Part(
                        text=(
                            f"Here is the HTML of the page at {task.url}:\n\n"
                            f"```html\n{html}\n```\n\n"
                            f"And here is a screenshot of the page:"
                        )
                    ),
                    types.Part(
                        inline_data=types.Blob(
                            mime_type="image/png",
                            data=screenshot_bytes,
                        )
                    ),
                    types.Part(text=task.prompt),
                ]
            )
        ]

        t0 = time.monotonic()
        response = self._client.models.generate_content(
            model=self._effective_model(),
            contents=contents,
            config=types.GenerateContentConfig(
                system_instruction=SYSTEM_PROMPT,
                max_output_tokens=4096,
            ),
        )
        elapsed = time.monotonic() - t0

        usage = response.usage_metadata
        metrics.turns.append(TurnMetrics(
            turn=0,
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

        metrics.final_answer = "\n".join(text_parts)
        metrics.finalize()
        return metrics

    async def teardown(self) -> None:
        if self._browser:
            await self._browser.close()
        if self._playwright:
            await self._playwright.stop()
