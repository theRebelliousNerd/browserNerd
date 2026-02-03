"""Metrics dataclasses for per-turn and per-task measurements."""

from __future__ import annotations

from dataclasses import dataclass, field


@dataclass
class TurnMetrics:
    """Metrics for a single API round-trip."""

    turn: int
    input_tokens: int
    output_tokens: int
    tool_calls: int
    wall_clock_s: float
    tools_called: list[str] = field(default_factory=list)  # Tool names called this turn


@dataclass
class TaskMetrics:
    """Aggregated metrics for one (task, condition, run) execution."""

    task_id: str
    condition: str
    run: int
    turns: list[TurnMetrics] = field(default_factory=list)
    total_input_tokens: int = 0
    total_output_tokens: int = 0
    total_tool_calls: int = 0
    total_wall_clock_s: float = 0.0
    final_answer: str = ""
    correct: bool | None = None  # None = not yet judged
    tool_usage: dict[str, int] = field(default_factory=dict)  # Tool name -> call count
    error_traceback: str | None = None  # Full traceback if task failed

    def finalize(self) -> None:
        """Roll up per-turn metrics into totals."""
        self.total_input_tokens = sum(t.input_tokens for t in self.turns)
        self.total_output_tokens = sum(t.output_tokens for t in self.turns)
        self.total_tool_calls = sum(t.tool_calls for t in self.turns)
        self.total_wall_clock_s = sum(t.wall_clock_s for t in self.turns)
        # Aggregate tool usage across turns
        self.tool_usage = {}
        for t in self.turns:
            for tool_name in t.tools_called:
                self.tool_usage[tool_name] = self.tool_usage.get(tool_name, 0) + 1
