"""Orchestrator: loads config, iterates conditions x tasks x runs."""

from __future__ import annotations

import asyncio
import json
import logging
import traceback

from config import Condition, HarnessSettings, TaskDefaults, TaskFile
from judge import evaluate
from metrics import TaskMetrics
from runners.base import BaseRunner
from runners.browsernerd_mcp import BrowserNERDMCPRunner
from runners.chrome_devtools_mcp import ChromeDevToolsMCPRunner
from runners.html_screenshot import HTMLScreenshotRunner
from runners.playwright_mcp import PlaywrightMCPRunner
from runners.puppeteer_mcp import PuppeteerMCPRunner
from runners.raw_html import RawHTMLRunner

logger = logging.getLogger(__name__)

RUNNER_MAP: dict[Condition, type[BaseRunner]] = {
    Condition.browsernerd_mcp: BrowserNERDMCPRunner,
    Condition.raw_html: RawHTMLRunner,
    Condition.html_screenshot: HTMLScreenshotRunner,
    Condition.puppeteer_mcp: PuppeteerMCPRunner,
    Condition.playwright_mcp: PlaywrightMCPRunner,
    Condition.chrome_devtools_mcp: ChromeDevToolsMCPRunner,
}


def load_task_file(settings: HarnessSettings) -> TaskFile:
    raw = json.loads(settings.tasks_path.read_text())
    return TaskFile.model_validate(raw)


async def run_harness(settings: HarnessSettings) -> list[TaskMetrics]:
    """Run the full evaluation and return all metrics."""
    task_file = load_task_file(settings)
    defaults = task_file.defaults

    # Apply CLI overrides
    if settings.model:
        defaults.model = settings.model
    if settings.max_turns:
        defaults.max_turns = settings.max_turns
    if settings.num_runs:
        defaults.num_runs = settings.num_runs

    all_metrics: list[TaskMetrics] = []

    for condition in settings.conditions:
        runner_cls = RUNNER_MAP[condition]
        runner = runner_cls(settings, defaults)

        logger.info("=== Condition: %s ===", condition.value)
        await runner.setup()

        try:
            for task in task_file.tasks:
                for run_idx in range(defaults.num_runs):
                    logger.info(
                        "  Task %s, run %d/%d",
                        task.id,
                        run_idx + 1,
                        defaults.num_runs,
                    )
                    try:
                        metrics = await runner.run_task(task, run_idx)
                        # Judge the answer
                        metrics.correct = evaluate(metrics.final_answer, task.ground_truth)
                        logger.info(
                            "    -> %d tokens (%d in + %d out), %d tool calls, %.1fs, correct=%s",
                            metrics.total_input_tokens + metrics.total_output_tokens,
                            metrics.total_input_tokens,
                            metrics.total_output_tokens,
                            metrics.total_tool_calls,
                            metrics.total_wall_clock_s,
                            metrics.correct,
                        )
                    except Exception as e:
                        error_detail = f"{type(e).__name__}: {e}"
                        full_traceback = traceback.format_exc()
                        logger.exception("    Task %s run %d failed: %s", task.id, run_idx, error_detail)
                        metrics = TaskMetrics(
                            task_id=task.id,
                            condition=condition.value,
                            run=run_idx,
                            final_answer=f"ERROR: {error_detail}",
                            correct=False,
                            error_traceback=full_traceback,
                        )
                    all_metrics.append(metrics)
        finally:
            await runner.teardown()

    return all_metrics
