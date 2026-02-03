"""Answer evaluation: exact, contains, regex, llm_judge."""

from __future__ import annotations

import logging
import re

import anthropic

from config import GroundTruth, GroundTruthType

logger = logging.getLogger(__name__)


def evaluate(answer: str, ground_truth: GroundTruth) -> bool:
    """Check whether ``answer`` satisfies ``ground_truth``."""
    match ground_truth.type:
        case GroundTruthType.exact:
            return _exact(answer, ground_truth.value)
        case GroundTruthType.contains:
            return _contains(answer, ground_truth.value)
        case GroundTruthType.regex:
            return _regex(answer, ground_truth.value)
        case GroundTruthType.llm_judge:
            return _llm_judge(answer, ground_truth.value)


def _exact(answer: str, expected: str) -> bool:
    return answer.strip().lower() == expected.strip().lower()


def _contains(answer: str, expected: str) -> bool:
    return expected.strip().lower() in answer.strip().lower()


def _regex(answer: str, pattern: str) -> bool:
    return bool(re.search(pattern, answer, re.IGNORECASE | re.DOTALL))


def _llm_judge(answer: str, criteria: str) -> bool:
    """Use Claude as a judge to evaluate the answer against criteria."""
    client = anthropic.Anthropic()
    response = client.messages.create(
        model="claude-sonnet-4-20250514",
        max_tokens=16,
        system=(
            "You are an evaluation judge. Respond with exactly YES or NO. "
            "Does the answer satisfy the given criteria?"
        ),
        messages=[{
            "role": "user",
            "content": (
                f"Criteria: {criteria}\n\n"
                f"Answer to evaluate:\n{answer}"
            ),
        }],
    )
    text = response.content[0].text.strip().upper()
    return text.startswith("YES")
