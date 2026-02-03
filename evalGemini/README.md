# BrowserNERD Evaluation Harness (Gemini 3)

Compares information extraction using BrowserNERD MCP against three baselines,
measuring token usage, tool calls, wall-clock time, and accuracy.

**Uses Google Gemini 3 Flash Preview** ($0.50/$3 per 1M tokens, 1M context).

## Conditions

| # | Condition | What happens |
|---|-----------|-------------|
| 1 | `browsernerd_mcp` | Gemini 3 uses BrowserNERD's 34 tools via stdio MCP |
| 2 | `raw_html` | Page fetched with httpx, full HTML passed as context |
| 3 | `html_screenshot` | Playwright fetches HTML + viewport screenshot, both sent to Gemini 3 |
| 4 | `puppeteer_mcp` | Gemini 3 uses a Puppeteer-based MCP server |

## Prerequisites

- Python 3.11+
- [uv](https://docs.astral.sh/uv/)
- `GEMINI_API_KEY` environment variable set
- Go toolchain (to build BrowserNERD)
- A running Chrome/Chromium instance with remote debugging (for `browsernerd_mcp`)

## Setup

```bash
cd evalGemini

# Install dependencies and create the virtual environment
uv sync

# Install Playwright's Chromium browser (needed for html_screenshot condition)
uv run playwright install chromium

# Build the BrowserNERD MCP server (needed for browsernerd_mcp condition)
cd ../mcp-server
go build -o bin/browsernerd ./cmd/server
cd ../evalGemini

# Set your Gemini API key
export GEMINI_API_KEY="your-api-key-here"
```

## Usage

### Run a single condition (no MCP server needed)

```bash
uv run browsernerd-eval-gemini --tasks tasks/example_tasks.json --conditions raw_html
```

### Run the BrowserNERD MCP condition

```bash
uv run browsernerd-eval-gemini --tasks tasks/example_tasks.json \
  --conditions browsernerd_mcp \
  --browsernerd-binary ../mcp-server/bin/browsernerd \
  --browsernerd-config ../mcp-server/config.yaml
```

### Run all conditions

```bash
uv run browsernerd-eval-gemini --tasks tasks/example_tasks.json \
  --browsernerd-binary ../mcp-server/bin/browsernerd \
  --browsernerd-config ../mcp-server/config.yaml \
  --puppeteer-command npx \
  --puppeteer-args @anthropic/puppeteer-mcp
```

### Flags

| Flag | Effect |
|---|---|
| `--num-runs 1` | Single run per task (faster iteration) |
| `--max-turns 5` | Fewer conversation turns |
| `--model gemini-3-pro-preview` | Use Gemini 3 Pro instead of Flash |
| `--output-dir my_results` | Change output directory (default: `results/`) |
| `-v` | Debug logging |

## Output

Results are written to `results/` (or the path given by `--output-dir`):

- **results.csv** - one row per (task, condition, run) with token totals
- **results.json** - full per-turn breakdown including tool usage
- **report.md** - summary table with means, 95% confidence intervals, token ratios, and tool usage analysis

## Task format

Tasks are defined in a JSON file (same format as the Anthropic version):

```json
{
  "defaults": {
    "model": "gemini-3-flash-preview",
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

## Token Counting

Token usage is tracked via `response.usage_metadata`:
- `prompt_token_count` - input tokens (includes system instructions and tools)
- `candidates_token_count` - output tokens
- `total_token_count` - combined total

## Differences from Anthropic Version

1. Uses `google-genai` library instead of `anthropic`
2. Default model is `gemini-3-flash-preview`
3. Function responses use `role="tool"` in conversation history
4. Images use `types.Part(inline_data=types.Blob(...))` format
5. LLM judge uses Gemini 3 Flash for evaluation
