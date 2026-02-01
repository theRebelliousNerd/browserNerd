@echo off
REM Start Chrome with remote debugging for BrowserNERD MCP Server
REM This creates a separate Chrome profile for debugging

echo Starting Chrome with remote debugging on port 9222...
echo.
echo IMPORTANT: Keep this window open while using BrowserNERD
echo Close this window to stop the debug Chrome instance
echo.

REM Create temp directory for Chrome profile
if not exist "C:\temp\chrome-debug" mkdir "C:\temp\chrome-debug"

REM Start Chrome with remote debugging
REM Adjust path if Chrome is installed elsewhere
start "" "C:\Program Files\Google\Chrome\Application\chrome.exe" ^
  --remote-debugging-port=9222 ^
  --user-data-dir="C:\temp\chrome-debug" ^
  --no-first-run ^
  --no-default-browser-check

echo Chrome started with remote debugging enabled
echo You can now use Claude Code with BrowserNERD MCP tools
pause
