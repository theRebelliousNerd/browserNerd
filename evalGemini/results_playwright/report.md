# BrowserNERD Evaluation Report (Gemini 3)

## Summary

| Condition | Runs | Success Rate | Avg Total Tokens (95% CI) | Avg Tool Calls (95% CI) | Avg Wall Clock (95% CI) |
|---|---|---|---|---|---|
| playwright_mcp | 8 | 3/8 (38%) | 156,945 [28,014, 285,876] | 3.2 [1.9, 4.6] | 15.6s [6.2s, 25.0s] |

## Per-Task Results

### docs_search_and_navigate

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| playwright_mcp | 0 | 235,488 | 5 | 33.8s | False |

### expand_accordion_content

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| playwright_mcp | 0 | 27,748 | 2 | 7.7s | False |

### github_repo_license

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| playwright_mcp | 0 | 22,679 | 1 | 3.2s | False |

### hn_second_page_story

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| playwright_mcp | 0 | 67,962 | 2 | 6.1s | True |

### mdn_navigate_to_fetch

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| playwright_mcp | 0 | 456,680 | 4 | 20.7s | False |

### npm_package_dependency

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| playwright_mcp | 0 | 22,580 | 5 | 22.0s | True |

### react_docs_nested_page

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| playwright_mcp | 0 | 167,082 | 5 | 24.9s | False |

### wikipedia_linked_article

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| playwright_mcp | 0 | 255,342 | 2 | 6.1s | True |
