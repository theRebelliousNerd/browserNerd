"""Answer evaluation: exact, contains, regex, llm_judge (Gemini version)."""

from __future__ import annotations

import logging
import os
import re

from google import genai
from google.genai import types

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
        case _:
            return False


def _exact(answer: str, expected: str) -> bool:
    return answer.strip().lower() == expected.strip().lower()


def _contains(answer: str, expected: str) -> bool:
    return expected.strip().lower() in answer.strip().lower()


def _regex(answer: str, pattern: str) -> bool:
    return bool(re.search(pattern, answer, re.IGNORECASE | re.DOTALL))


def _llm_judge(answer: str, criteria: str) -> bool:
    """Use Gemini as a judge to evaluate the answer against criteria."""
    client = genai.Client(api_key=os.environ.get("GEMINI_API_KEY"))

    response = client.models.generate_content(
        model="gemini-3-flash-preview",
        contents=(
            f"Criteria: {criteria}\n\n"
            f"Answer to evaluate:\n{answer}"
        ),
        config=types.GenerateContentConfig(
            system_instruction=(
                "You are an evaluation judge. Respond with exactly YES or NO. "
                "Does the answer satisfy the given criteria?"
            ),
            max_output_tokens=16,
        ),
    )

    text = ""
    if response.candidates:
        for part in response.candidates[0].content.parts or []:
            if part.text:
                text = part.text
                break

    return text.strip().upper().startswith("YES")
