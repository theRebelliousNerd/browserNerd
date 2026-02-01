# BrowserNERD Integration Tests

This document describes the comprehensive integration test suite for BrowserNERD that requires a live Chrome browser.

## Overview

The integration tests provide comprehensive coverage for browser-dependent functionality that cannot be tested with unit tests alone. These tests are **skipped by default** when `SKIP_LIVE_TESTS` environment variable is set.

## Current Coverage

**Unit Tests (SKIP_LIVE_TESTS=1):** 50.2% overall
- config: 100.0%
- mangle: 85.9%
- docker: 81.2%
- mcp: 46.4%
- browser: 26.8%
- cmd/server: 0.0%

**With Integration Tests:** Estimated 80%+ overall coverage when run with a live browser.

## Test Files

### Browser Session Management
**File:** `internal/browser/session_manager_integration_test.go`

Tests:
- Browser startup and connection
- Session creation and management
- Session listing and retrieval
- Page navigation and waiting
- DOM snapshot capture
- Session forking
- Attaching to existing targets
- Browser reconnection
- React fiber tree extraction (ReifyReact)
- Session persistence

### Navigation Tools
**File:** `internal/mcp/navigation_integration_test.go`

Tests:
- GetPageStateTool - Page state retrieval
- NavigateURLTool - URL navigation with wait strategies
- GetInteractiveElementsTool - Interactive element discovery with filters
- GetNavigationLinksTool - Link extraction
- EvaluateJSTool - JavaScript execution and error handling
- SmartScreenshotTool - Full page and element screenshots
- BrowserHistoryTool - Back, forward, reload
- DiscoverHiddenContentTool - Hidden element discovery

### Interaction Tools
**File:** `internal/mcp/navigation_integration_test.go`

Tests:
- InteractTool - Click, type, select, toggle actions
- PressKeyTool - Keyboard input with modifiers
- FillFormTool - Multi-field form filling

### Automation Tools
**File:** `internal/mcp/automation_integration_test.go`

Tests:
- ExecutePlanTool - Multi-step action sequences
  - Explicit action execution
  - Stop on error behavior
  - Continue on error behavior
- WaitForConditionTool - Condition waiting
  - Element exists
  - Element visible
  - Text contains
  - Custom JavaScript conditions
  - Timeout handling

### Session Management Tools
**File:** `internal/mcp/automation_integration_test.go`

Tests:
- LaunchBrowserTool
- ListSessionsTool
- CreateSessionTool with metadata
- ForkSessionTool
- SnapshotDOMTool
- ReifyReactTool
- ShutdownBrowserTool

### Element Finding Helpers
**File:** `internal/mcp/helpers_integration_test.go`

Tests:
- findElementByRef strategies:
  - ID matching
  - Name attribute matching
  - data-testid prefix
  - aria-label prefix
  - CSS selectors
- findElementByRefWithRegistry with fingerprints:
  - data-testid from fingerprint
  - aria-label from fingerprint
  - ID from fingerprint
  - Name from fingerprint
  - Fallback strategies
- validateFingerprint:
  - Nil fingerprint handling
  - Matching fingerprints
  - Changed attributes detection
  - Position changes
  - Score calculation

### Server Lifecycle
**File:** `cmd/server/main_integration_test.go`

Tests:
- Configuration loading
- Mangle engine initialization
- Session manager initialization
- MCP server initialization
- Full server lifecycle with browser
- Tool execution workflow
- Browser shutdown
- Docker-enabled configuration

## Running the Tests

### Prerequisites

1. **Chrome/Chromium browser** installed and accessible in PATH
2. **Go 1.23+** installed
3. **Network access** (tests use data URLs, minimal network needed)

### Run Integration Tests

```bash
# Navigate to mcp-server directory
cd dev_tools/BrowserNERD/mcp-server

# Run ALL tests including integration tests
export GOTOOLCHAIN=local
unset SKIP_LIVE_TESTS
go test -v -coverprofile=coverage_full.out -covermode=count ./...

# Run only integration tests for a specific package
go test -v ./internal/browser -run Integration

# Run with coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### Run Unit Tests Only (Default)

```bash
export GOTOOLCHAIN=local
export SKIP_LIVE_TESTS=1
go test -v -coverprofile=coverage.out -covermode=count ./...
```

### Run Specific Integration Test

```bash
unset SKIP_LIVE_TESTS

# Browser session tests
go test -v ./internal/browser -run TestIntegrationSessionManager

# Navigation tools
go test -v ./internal/mcp -run TestIntegrationNavigationTools

# Automation tools
go test -v ./internal/mcp -run TestIntegrationExecutePlan

# Element finding
go test -v ./internal/mcp -run TestIntegrationFindElementByRef

# Server lifecycle
go test -v ./cmd/server -run TestIntegrationServerLifecycle
```

## Test Configuration

### Browser Configuration

Tests use headless Chrome by default:

```go
cfg := config.BrowserConfig{
    Headless:              boolPtr(true),
    EnableDOMIngestion:    true,
    EnableHeaderIngestion: true,
    EventThrottleMs:       50,
}
```

To run with headed browser for debugging:
```go
Headless: boolPtr(false),
```

### Chrome Binary Location

If Chrome is not in PATH, you can specify the binary location:

```go
cfg.Launch = []string{"/path/to/chrome", "--headless"}
```

Or set via environment variable before running tests:
```bash
export CHROME_BIN=/usr/bin/google-chrome
```

## Debugging Integration Tests

### Enable Verbose Output

```bash
go test -v -run TestIntegrationNavigationTools ./internal/mcp
```

### Run with Browser UI (Non-Headless)

Modify test configuration:
```go
Headless: boolPtr(false),
```

### Inspect Test HTML

Tests use data URLs with inline HTML. To inspect the page:

1. Set headless to false
2. Add a sleep after page load:
   ```go
   time.Sleep(30 * time.Second) // Keep browser open
   ```
3. Navigate manually to inspect

### Check Browser Logs

Enable browser console logging:
```go
cfg.Browser.EnableDOMIngestion = true
```

Browser events will be logged to stdout.

## Common Issues

### Chrome Not Found

```
Error: Failed to start browser: exec: "chrome": executable file not found
```

**Solution:** Install Chrome/Chromium or set CHROME_BIN:
```bash
# Ubuntu/Debian
sudo apt-get install chromium-browser

# macOS
brew install --cask google-chrome

# Or specify path
export CHROME_BIN=/usr/bin/chromium-browser
```

### Port Already in Use

```
Error: Failed to start browser: listen tcp :9222: bind: address already in use
```

**Solution:** Kill existing Chrome debug instances:
```bash
pkill -f "chrome.*remote-debugging-port"
```

### Tests Timeout

```
Error: context deadline exceeded
```

**Solution:** Increase timeout in test:
```go
ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
```

### Flaky Tests

Some tests may be timing-dependent. Increase delays if tests are flaky:
```go
time.Sleep(500 * time.Millisecond) // Increase wait time
```

## Coverage Report

Generate detailed coverage report:

```bash
# Run all tests with coverage
unset SKIP_LIVE_TESTS
go test -coverprofile=coverage.out -covermode=count ./...

# Generate HTML report
go tool cover -html=coverage.out -o coverage.html

# View in browser
open coverage.html # macOS
xdg-open coverage.html # Linux
```

### Expected Coverage with Integration Tests

| Package | Unit Only | With Integration |
|---------|-----------|------------------|
| config | 100.0% | 100.0% |
| mangle | 85.9% | 85.9% |
| docker | 81.2% | 81.2% |
| **mcp** | **46.4%** | **~85%** |
| **browser** | **26.8%** | **~85%** |
| **cmd/server** | **0.0%** | **~70%** |
| **Overall** | **50.2%** | **~80-85%** |

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Integration Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Install Chrome
        run: |
          wget -q -O - https://dl-ssl.google.com/linux/linux_signing_key.pub | sudo apt-key add -
          sudo sh -c 'echo "deb [arch=amd64] http://dl.google.com/linux/chrome/deb/ stable main" >> /etc/apt/sources.list.d/google.list'
          sudo apt-get update
          sudo apt-get install google-chrome-stable

      - name: Setup Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.23'

      - name: Run Integration Tests
        working-directory: dev_tools/BrowserNERD/mcp-server
        run: |
          export GOTOOLCHAIN=local
          unset SKIP_LIVE_TESTS
          go test -v -coverprofile=coverage.out ./...

      - name: Upload Coverage
        uses: codecov/codecov-action@v3
        with:
          files: ./dev_tools/BrowserNERD/mcp-server/coverage.out
```

## Writing New Integration Tests

### Template

```go
func TestIntegrationYourFeature(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping integration tests (SKIP_LIVE_TESTS set)")
	}

	cfg := setupIntegrationConfig()
	engine := setupIntegrationEngine(t, cfg)
	sessions := setupIntegrationBrowser(t, cfg, engine)
	defer sessions.Shutdown(context.Background())

	ctx := context.Background()

	// Your test HTML
	testHTML := `<!DOCTYPE html>
	<html>
	<body><h1>Test</h1></body>
	</html>`

	session, err := sessions.CreateSession(ctx, "about:blank", nil)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	page, _ := sessions.Page(session.ID)
	dataURL := "data:text/html;charset=utf-8," + testHTML
	err = page.Navigate(dataURL)
	if err != nil {
		t.Fatalf("Navigate failed: %v", err)
	}
	err = page.WaitLoad()
	if err != nil {
		t.Fatalf("WaitLoad failed: %v", err)
	}

	t.Run("your test case", func(t *testing.T) {
		// Your test logic here
	})
}
```

### Best Practices

1. **Always check SKIP_LIVE_TESTS** at the start
2. **Use setupIntegration* helpers** for consistent setup
3. **Use data URLs** for self-contained tests
4. **Clean up** with defer for browser shutdown
5. **Add appropriate timeouts** to prevent hanging
6. **Test both success and error paths**
7. **Verify results** with specific assertions, not just "no error"

## Maintenance

### Updating Tests

When browser or tool behavior changes:

1. Run integration tests locally to catch failures
2. Update test expectations to match new behavior
3. Document breaking changes in commit messages
4. Update this README if test setup changes

### Adding New Tools

When adding new MCP tools:

1. Create unit tests for basic validation
2. Create integration test for browser interaction
3. Follow the template above
4. Update coverage expectations in this README

## Support

For issues with integration tests:

1. Check Chrome is installed: `which chrome || which chromium-browser`
2. Verify Go version: `go version` (need 1.23+)
3. Run with `-v` flag for verbose output
4. Check test logs for specific error messages
5. Try running tests individually to isolate failures

## License

Same as BrowserNERD main project.
