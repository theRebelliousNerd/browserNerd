# BrowserNERD Evaluation Report (Gemini 3)

## Summary

| Condition | Runs | Success Rate | Avg Total Tokens (95% CI) | Avg Tool Calls (95% CI) | Avg Wall Clock (95% CI) |
|---|---|---|---|---|---|
| browsernerd_mcp | 8 | 4/8 (50%) | 137,945 [69,701, 206,188] | 9.6 [5.8, 13.5] | 23.0s [12.3s, 33.7s] |

## Tool Usage Analysis (BrowserNERD)

Shows which tools were called and how often.

| Tool | Calls | % of Total |
|---|---|---|
| evaluate-js | 23 | 29.9% |
| get-interactive-elements | 11 | 14.3% |
| launch-browser | 8 | 10.4% |
| create-session | 8 | 10.4% |
| interact | 7 | 9.1% |
| get-navigation-links | 6 | 7.8% |
| navigate-url | 6 | 7.8% |
| await-stable-state | 4 | 5.2% |
| get-page-state | 2 | 2.6% |
| press-key | 1 | 1.3% |
| screenshot | 1 | 1.3% |

### Token Cost Compliance

- LOW cost tools: 31 calls (40.3%)
- MEDIUM cost tools: 11 calls (14.3%)
- HIGH cost tools: 1 calls (1.3%)

Good: Screenshot usage is minimal (1.3%)

## Per-Task Results

### docs_search_and_navigate

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| browsernerd_mcp | 0 | 226,859 | 15 | 35.3s | False |

### expand_accordion_content

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| browsernerd_mcp | 0 | 69,878 | 6 | 13.6s | False |

### github_repo_license

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| browsernerd_mcp | 0 | 47,693 | 4 | 9.3s | True |

### hn_second_page_story

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| browsernerd_mcp | 0 | 77,128 | 6 | 13.2s | True |

### mdn_navigate_to_fetch

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| browsernerd_mcp | 0 | 245,563 | 15 | 36.7s | False |

### npm_package_dependency

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| browsernerd_mcp | 0 | 103,710 | 8 | 17.2s | True |

### react_docs_nested_page

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| browsernerd_mcp | 0 | 229,178 | 15 | 42.1s | False |

### wikipedia_linked_article

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| browsernerd_mcp | 0 | 103,549 | 8 | 16.4s | True |
