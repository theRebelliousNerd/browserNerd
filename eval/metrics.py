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

    def finalize(self) -> None:
        """Roll up per-turn metrics into totals."""
        self.total_input_tokens = sum(t.input_tokens for t in self.turns)
        self.total_output_tokens = sum(t.output_tokens for t in self.turns)
        self.total_tool_calls = sum(t.tool_calls for t in self.turns)
        self.total_wall_clock_s = sum(t.wall_clock_s for t in self.turns)
