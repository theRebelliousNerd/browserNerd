"""Pydantic models for task configuration and harness settings."""

from __future__ import annotations

from enum import Enum
from pathlib import Path
from typing import Literal

from pydantic import BaseModel, Field


class GroundTruthType(str, Enum):
    exact = "exact"
    contains = "contains"
    regex = "regex"
    llm_judge = "llm_judge"


class GroundTruth(BaseModel):
    type: GroundTruthType
    value: str


class TaskDef(BaseModel):
    id: str
    url: str
    prompt: str
    ground_truth: GroundTruth


class TaskDefaults(BaseModel):
    # Gemini 3 Flash: gemini-3-flash-preview (1M/64k context, $0.50/$3 per 1M tokens)
    model: str = "gemini-3-flash-preview"
    max_turns: int = 10
    num_runs: int = 3


class TaskFile(BaseModel):
    defaults: TaskDefaults = Field(default_factory=TaskDefaults)
    tasks: list[TaskDef]


class Condition(str, Enum):
    browsernerd_mcp = "browsernerd_mcp"
    raw_html = "raw_html"
    html_screenshot = "html_screenshot"
    puppeteer_mcp = "puppeteer_mcp"
    playwright_mcp = "playwright_mcp"
    chrome_devtools_mcp = "chrome_devtools_mcp"


class HarnessSettings(BaseModel):
    """Runtime settings assembled from CLI args."""

    tasks_path: Path
    conditions: list[Condition]
    output_dir: Path = Path("results")
    model: str | None = None  # override TaskDefaults.model
    max_turns: int | None = None  # override TaskDefaults.max_turns
    num_runs: int | None = None  # override TaskDefaults.num_runs

    # BrowserNERD-specific
    browsernerd_binary: Path | None = None
    browsernerd_config: Path | None = None

    # Puppeteer MCP-specific
    puppeteer_command: str | None = None
    puppeteer_args: list[str] = Field(default_factory=list)

    # Playwright MCP-specific
    playwright_command: str | None = None
    playwright_args: list[str] = Field(default_factory=list)

    # Chrome DevTools MCP-specific
    chrome_devtools_command: str | None = None
    chrome_devtools_args: list[str] = Field(default_factory=list)
