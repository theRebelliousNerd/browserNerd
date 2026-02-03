"""Abstract base runner interface."""

from __future__ import annotations

import abc

from config import HarnessSettings, TaskDef, TaskDefaults
from metrics import TaskMetrics


class BaseRunner(abc.ABC):
    """Every condition implements setup → run_task → teardown."""

    def __init__(self, settings: HarnessSettings, defaults: TaskDefaults) -> None:
        self.settings = settings
        self.defaults = defaults

    @property
    def condition_name(self) -> str:
        return type(self).__name__

    @abc.abstractmethod
    async def setup(self) -> None:
        """One-time setup before running any tasks (e.g. start MCP server)."""

    @abc.abstractmethod
    async def run_task(self, task: TaskDef, run: int) -> TaskMetrics:
        """Execute a single task and return metrics."""

    @abc.abstractmethod
    async def teardown(self) -> None:
        """Cleanup after all tasks are done (e.g. stop MCP server)."""

    def _effective_model(self) -> str:
        return self.settings.model or self.defaults.model

    def _effective_max_turns(self) -> int:
        return self.settings.max_turns or self.defaults.max_turns
