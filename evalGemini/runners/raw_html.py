"""Condition 2: Raw HTML baseline - fetch with httpx, pass full HTML as context (Gemini version)."""

from __future__ import annotations

import logging
import os
import time

from google import genai
from google.genai import types
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
        api_key = os.environ.get("GEMINI_API_KEY")
        if not api_key:
            raise ValueError("GEMINI_API_KEY environment variable is required")
        self._client = genai.Client(api_key=api_key)

    @property
    def condition_name(self) -> str:
        return "raw_html"

    async def setup(self) -> None:
        pass

    async def run_task(self, task: TaskDef, run: int) -> TaskMetrics:
        metrics = TaskMetrics(task_id=task.id, condition=self.condition_name, run=run)

        # Fetch HTML (with User-Agent to avoid 403s from sites like Wikipedia)
        headers = {"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"}
        async with httpx.AsyncClient(follow_redirects=True, timeout=30, headers=headers) as http:
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
        response = self._client.models.generate_content(
            model=self._effective_model(),
            contents=user_message,
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
        pass
