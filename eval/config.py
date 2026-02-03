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
    model: str = "claude-sonnet-4-20250514"
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
