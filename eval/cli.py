"""CLI entry point for the BrowserNERD evaluation harness."""

from __future__ import annotations

import argparse
import asyncio
import logging
import sys
from pathlib import Path

from config import Condition, HarnessSettings
from harness import run_harness
from report import write_results


def parse_args(argv: list[str] | None = None) -> HarnessSettings:
    parser = argparse.ArgumentParser(
        prog="browsernerd-eval",
        description="BrowserNERD evaluation harness: compare MCP vs baselines",
    )
    parser.add_argument(
        "--tasks",
        type=Path,
        required=True,
        help="Path to task definitions JSON file",
    )
    parser.add_argument(
        "--conditions",
        nargs="+",
        choices=[c.value for c in Condition],
        default=None,
        help="Conditions to run (default: all)",
    )
    parser.add_argument(
        "--output-dir",
        type=Path,
        default=Path("results"),
        help="Output directory for results (default: results/)",
    )
    parser.add_argument("--model", type=str, default=None, help="Override model")
    parser.add_argument("--max-turns", type=int, default=None, help="Override max turns")
    parser.add_argument("--num-runs", type=int, default=None, help="Override number of runs per task")

    # BrowserNERD MCP options
    parser.add_argument(
        "--browsernerd-binary",
        type=Path,
        default=None,
        help="Path to BrowserNERD MCP server binary",
    )
    parser.add_argument(
        "--browsernerd-config",
        type=Path,
        default=None,
        help="Path to BrowserNERD config.yaml",
    )

    # Puppeteer MCP options
    parser.add_argument(
        "--puppeteer-command",
        type=str,
        default=None,
        help="Command to start Puppeteer MCP server",
    )
    parser.add_argument(
        "--puppeteer-args",
        nargs="*",
        default=[],
        help="Extra args for Puppeteer MCP server",
    )

    parser.add_argument(
        "-v", "--verbose",
        action="store_true",
        help="Enable debug logging",
    )

    args = parser.parse_args(argv)

    conditions = (
        [Condition(c) for c in args.conditions]
        if args.conditions
        else list(Condition)
    )

    return HarnessSettings(
        tasks_path=args.tasks,
        conditions=conditions,
        output_dir=args.output_dir,
        model=args.model,
        max_turns=args.max_turns,
        num_runs=args.num_runs,
        browsernerd_binary=args.browsernerd_binary,
        browsernerd_config=args.browsernerd_config,
        puppeteer_command=args.puppeteer_command,
        puppeteer_args=args.puppeteer_args,
    ), args.verbose


def main(argv: list[str] | None = None) -> None:
    settings, verbose = parse_args(argv)

    logging.basicConfig(
        level=logging.DEBUG if verbose else logging.INFO,
        format="%(asctime)s %(levelname)-8s %(name)s: %(message)s",
    )

    results = asyncio.run(run_harness(settings))
    write_results(results, settings.output_dir)

    total = len(results)
    correct = sum(1 for r in results if r.correct)
    print(f"\nDone: {correct}/{total} correct across all conditions.")
    print(f"Results in: {settings.output_dir}")


if __name__ == "__main__":
    main()
