# BrowserNERD Evaluation Report (Gemini 3)

## Summary

| Condition | Runs | Success Rate | Avg Total Tokens (95% CI) | Avg Tool Calls (95% CI) | Avg Wall Clock (95% CI) |
|---|---|---|---|---|---|
| chrome_devtools_mcp | 8 | 3/8 (38%) | 142,054 [40,855, 243,252] | 4.2 [3.4, 5.1] | 11.0s [6.0s, 15.9s] |

## Per-Task Results

### docs_search_and_navigate

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| chrome_devtools_mcp | 0 | 91,930 | 5 | 23.9s | False |

### expand_accordion_content

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| chrome_devtools_mcp | 0 | 162,746 | 4 | 11.4s | False |

### github_repo_license

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| chrome_devtools_mcp | 0 | 33,605 | 2 | 3.9s | True |

### hn_second_page_story

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| chrome_devtools_mcp | 0 | 88,307 | 4 | 6.8s | False |

### mdn_navigate_to_fetch

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| chrome_devtools_mcp | 0 | 223,979 | 5 | 12.2s | False |

### npm_package_dependency

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| chrome_devtools_mcp | 0 | 25,848 | 5 | 11.0s | True |

### react_docs_nested_page

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| chrome_devtools_mcp | 0 | 114,926 | 5 | 9.7s | False |

### wikipedia_linked_article

| Condition | Run | Tokens | Tool Calls | Time | Correct |
|---|---|---|---|---|---|
| chrome_devtools_mcp | 0 | 395,087 | 4 | 8.6s | True |
