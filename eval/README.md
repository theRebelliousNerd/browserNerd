# BrowserNERD Evaluation Harness

Compares information extraction using BrowserNERD MCP against three baselines,
measuring token usage, tool calls, wall-clock time, and accuracy.

## Conditions

| # | Condition | What happens |
|---|-----------|-------------|
| 1 | `browsernerd_mcp` | Claude uses BrowserNERD's 34 tools via stdio MCP |
| 2 | `raw_html` | Page fetched with httpx, full HTML passed as context |
| 3 | `html_screenshot` | Playwright fetches HTML + viewport screenshot, both sent to Claude |
| 4 | `puppeteer_mcp` | Claude uses a Puppeteer-based MCP server |

## Prerequisites

- Python 3.11+
- [uv](https://docs.astral.sh/uv/)
- `ANTHROPIC_API_KEY` environment variable set
- Go toolchain (to build BrowserNERD)
- A running Chrome/Chromium instance with remote debugging (for `browsernerd_mcp`)

## Setup

```bash
cd eval

# Install dependencies and create the virtual environment
uv sync

# Install Playwright's Chromium browser (needed for html_screenshot condition)
uv run playwright install chromium

# Build the BrowserNERD MCP server (needed for browsernerd_mcp condition)
cd ../mcp-server
go build -o bin/browsernerd ./cmd/server
cd ../eval
```

## Usage

### Run a single condition (no MCP server needed)

```bash
uv run browsernerd-eval --tasks tasks/example_tasks.json --conditions raw_html
```

### Run the BrowserNERD MCP condition

```bash
uv run browsernerd-eval --tasks tasks/example_tasks.json \
  --conditions browsernerd_mcp \
  --browsernerd-binary ../mcp-server/bin/browsernerd \
  --browsernerd-config ../mcp-server/config.yaml
```

### Run all conditions

```bash
uv run browsernerd-eval --tasks tasks/example_tasks.json \
  --browsernerd-binary ../mcp-server/bin/browsernerd \
  --browsernerd-config ../mcp-server/config.yaml \
  --puppeteer-command npx \
  --puppeteer-args @anthropic/puppeteer-mcp
```

Omitting `--conditions` runs all four. Adjust `--puppeteer-command` /
`--puppeteer-args` to match your Puppeteer MCP server setup.

### Flags

| Flag | Effect |
|---|---|
| `--num-runs 1` | Single run per task (faster iteration) |
| `--max-turns 5` | Fewer conversation turns |
| `--model claude-sonnet-4-20250514` | Override model for all conditions |
| `--output-dir my_results` | Change output directory (default: `results/`) |
| `-v` | Debug logging |

## Output

Results are written to `results/` (or the path given by `--output-dir`):

- **results.csv** — one row per (task, condition, run) with token totals
- **results.json** — full per-turn breakdown
- **report.md** — summary table with means, 95% confidence intervals, token ratios, and per-task detail

## Task format

Tasks are defined in a JSON file:

```json
{
  "defaults": {
    "model": "claude-sonnet-4-20250514",
    "max_turns": 10,
    "num_runs": 3
  },
  "tasks": [
    {
      "id": "example_heading",
      "url": "https://example.com",
      "prompt": "What is the main heading?",
      "ground_truth": {"type": "exact", "value": "Example Domain"}
    }
  ]
}
```

Ground truth types: `exact`, `contains`, `regex`, `llm_judge`.
