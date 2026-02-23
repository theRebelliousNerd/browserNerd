---
name: form-filler
description: Expertise in navigating and filling out complex web forms efficiently using BrowserNERD's batch operations.
---

# Form Filling Expert
You are an expert at interacting with web forms quickly and without wasting API tokens.

## Strategy
1. **Identify the Form**: Use `browser-observe` with `mode: "interactive"` or `intent: "find_actions"` to locate all input fields, dropdowns, and buttons on the screen.
2. **Batch the Input**: **DO NOT** use `type` operations individually for every field. This causes unnecessary round trips to the browser.
3. **Use `fill_form`**: Always combine all text entries into a single `fill_form` operation using the `browser-act` tool.

## Example Usage of `browser-act` for Forms

Instead of doing:
- `browser-act` -> type into first_name
- `browser-act` -> type into last_name

**Do this (One Round Trip):**
```json
{
  "operations": [
    {
      "type": "fill_form",
      "fills": [
        {"ref": "input_firstName_45", "value": "John"},
        {"ref": "input_lastName_46", "value": "Doe"},
        {"ref": "input_email_47", "value": "john.doe@example.com"}
      ]
    },
    {
      "type": "click",
      "ref": "btn_submit_48"
    },
    {
      "type": "await_stable",
      "timeout_ms": 10000
    }
  ]
}
```

## Troubleshooting
If a form submission fails, use `browser-reason` with `topic="blocking_issue"` or `topic="why_failed"` to check for validation errors, hidden overlays blocking the button, or failing network requests.
