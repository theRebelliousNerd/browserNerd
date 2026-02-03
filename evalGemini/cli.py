"""CLI entry point for the BrowserNERD evaluation harness (Gemini 3 version)."""

from __future__ import annotations

import argparse
import asyncio
import logging
import shlex
from pathlib import Path

from dotenv import load_dotenv

from config import Condition, HarnessSettings
from harness import run_harness
from report import write_results

# Load .env from project root (3 levels up from this file: evalGemini -> BrowserNERD -> dev_tools -> symbiogenBackEndV3)
_PROJECT_ROOT = Path(__file__).resolve().parents[3]
load_dotenv(_PROJECT_ROOT / ".env")


def split_args(arg_string: str | None) -> list[str]:
    """Split a quoted arg string into a list, handling flags like --headless."""
    if not arg_string:
        return []
    return shlex.split(arg_string)


def parse_args(argv: list[str] | None = None) -> tuple[HarnessSettings, bool]:
    parser = argparse.ArgumentParser(
        prog="browsernerd-eval-gemini",
        description="BrowserNERD evaluation harness (Gemini 3): compare MCP vs baselines",
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
    parser.add_argument(
        "--model",
        type=str,
        default=None,
        help="Override model (default: gemini-3-flash-preview)",
    )
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
        type=str,
        default=None,
        help="Extra args for Puppeteer MCP server (quoted string, e.g. '--headless --flag')",
    )

    # Playwright MCP options
    parser.add_argument(
        "--playwright-command",
        type=str,
        default=None,
        help="Command to start Playwright MCP server",
    )
    parser.add_argument(
        "--playwright-args",
        type=str,
        default=None,
        help="Extra args for Playwright MCP server (quoted string, e.g. '--headless --flag')",
    )

    # Chrome DevTools MCP options
    parser.add_argument(
        "--chrome-devtools-command",
        type=str,
        default=None,
        help="Command to start Chrome DevTools MCP server",
    )
    parser.add_argument(
        "--chrome-devtools-args",
        type=str,
        default=None,
        help="Extra args for Chrome DevTools MCP server (quoted string, e.g. '--headless --isolated')",
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
        puppeteer_args=split_args(args.puppeteer_args),
        playwright_command=args.playwright_command,
        playwright_args=split_args(args.playwright_args),
        chrome_devtools_command=args.chrome_devtools_command,
        chrome_devtools_args=split_args(args.chrome_devtools_args),
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
