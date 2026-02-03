"""Generate results.csv, results.json, and report.md with stats and confidence intervals."""

from __future__ import annotations

import csv
import json
import logging
from collections import defaultdict
from dataclasses import asdict
from pathlib import Path

import scipy.stats as stats

from metrics import TaskMetrics

logger = logging.getLogger(__name__)


def _ci_95(values: list[float]) -> tuple[float, float, float]:
    """Return (mean, ci_low, ci_high) for a 95% confidence interval.

    Falls back to (mean, mean, mean) when sample size < 2.
    """
    n = len(values)
    if n == 0:
        return (0.0, 0.0, 0.0)
    mean = sum(values) / n
    if n < 2:
        return (mean, mean, mean)
    se = stats.sem(values)
    ci = stats.t.interval(0.95, df=n - 1, loc=mean, scale=se)
    return (mean, ci[0], ci[1])


def write_results(metrics: list[TaskMetrics], output_dir: Path) -> None:
    output_dir.mkdir(parents=True, exist_ok=True)
    _write_csv(metrics, output_dir / "results.csv")
    _write_json(metrics, output_dir / "results.json")
    _write_report(metrics, output_dir / "report.md")
    logger.info("Results written to %s", output_dir)


def _write_csv(metrics: list[TaskMetrics], path: Path) -> None:
    with path.open("w", newline="") as f:
        writer = csv.writer(f)
        writer.writerow([
            "task_id",
            "condition",
            "run",
            "input_tokens",
            "output_tokens",
            "total_tokens",
            "tool_calls",
            "wall_clock_s",
            "num_turns",
            "correct",
        ])
        for m in metrics:
            writer.writerow([
                m.task_id,
                m.condition,
                m.run,
                m.total_input_tokens,
                m.total_output_tokens,
                m.total_input_tokens + m.total_output_tokens,
                m.total_tool_calls,
                f"{m.total_wall_clock_s:.2f}",
                len(m.turns),
                m.correct,
            ])


def _write_json(metrics: list[TaskMetrics], path: Path) -> None:
    data = [asdict(m) for m in metrics]
    path.write_text(json.dumps(data, indent=2, default=str))


def _write_report(metrics: list[TaskMetrics], path: Path) -> None:
    # Group by condition
    by_condition: dict[str, list[TaskMetrics]] = defaultdict(list)
    for m in metrics:
        by_condition[m.condition].append(m)

    lines = ["# BrowserNERD Evaluation Report\n"]

    # Summary table
    lines.append("## Summary\n")
    lines.append(
        "| Condition | Runs | Success Rate | "
        "Avg Total Tokens (95% CI) | "
        "Avg Tool Calls (95% CI) | "
        "Avg Wall Clock (95% CI) |"
    )
    lines.append("|---|---|---|---|---|---|")

    # Compute raw_html token mean for ratio calculation
    raw_html_token_mean = None
    if "raw_html" in by_condition:
        raw_vals = [m.total_input_tokens + m.total_output_tokens for m in by_condition["raw_html"]]
        raw_html_token_mean = sum(raw_vals) / len(raw_vals) if raw_vals else None

    for cond, ms in sorted(by_condition.items()):
        n = len(ms)
        successes = sum(1 for m in ms if m.correct)
        success_rate = f"{successes}/{n} ({100 * successes / n:.0f}%)" if n else "N/A"

        tokens = [m.total_input_tokens + m.total_output_tokens for m in ms]
        tools = [float(m.total_tool_calls) for m in ms]
        clocks = [m.total_wall_clock_s for m in ms]

        tok_mean, tok_lo, tok_hi = _ci_95(tokens)
        tool_mean, tool_lo, tool_hi = _ci_95(tools)
        clk_mean, clk_lo, clk_hi = _ci_95(clocks)

        lines.append(
            f"| {cond} | {n} | {success_rate} | "
            f"{tok_mean:,.0f} [{tok_lo:,.0f}, {tok_hi:,.0f}] | "
            f"{tool_mean:.1f} [{tool_lo:.1f}, {tool_hi:.1f}] | "
            f"{clk_mean:.1f}s [{clk_lo:.1f}s, {clk_hi:.1f}s] |"
        )

    # Token ratio vs raw_html
    if raw_html_token_mean and raw_html_token_mean > 0:
        lines.append("\n## Token Ratio vs raw_html\n")
        lines.append("| Condition | Ratio |")
        lines.append("|---|---|")
        for cond, ms in sorted(by_condition.items()):
            tokens = [m.total_input_tokens + m.total_output_tokens for m in ms]
            mean = sum(tokens) / len(tokens) if tokens else 0
            ratio = mean / raw_html_token_mean
            lines.append(f"| {cond} | {ratio:.2f}x |")

    # Per-task breakdown
    lines.append("\n## Per-Task Results\n")
    by_task: dict[str, list[TaskMetrics]] = defaultdict(list)
    for m in metrics:
        by_task[m.task_id].append(m)

    for task_id, ms in sorted(by_task.items()):
        lines.append(f"### {task_id}\n")
        lines.append("| Condition | Run | Tokens | Tool Calls | Time | Correct |")
        lines.append("|---|---|---|---|---|---|")
        for m in sorted(ms, key=lambda x: (x.condition, x.run)):
            total_tok = m.total_input_tokens + m.total_output_tokens
            lines.append(
                f"| {m.condition} | {m.run} | {total_tok:,} | "
                f"{m.total_tool_calls} | {m.total_wall_clock_s:.1f}s | {m.correct} |"
            )
        lines.append("")

    path.write_text("\n".join(lines))
