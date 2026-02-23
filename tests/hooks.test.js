/**
 * Hooks Validation Tests
 * 
 * Validates that the Gemini CLI lifecycle hooks are correctly structured,
 * export the right function signatures, and handle edge cases.
 */
const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(__dirname, '..');
const HOOKS_DIR = path.join(ROOT, 'hooks');

describe('Hooks directory', () => {
  test('exists', () => {
    expect(fs.existsSync(HOOKS_DIR)).toBe(true);
  });

  test('contains at least one hook file', () => {
    const files = fs.readdirSync(HOOKS_DIR).filter(f => f.endsWith('.js'));
    expect(files.length).toBeGreaterThanOrEqual(1);
  });
});

describe('after-tool-call hook', () => {
  const hookPath = path.join(HOOKS_DIR, 'after-tool-call.js');
  let content;

  beforeAll(() => {
    content = fs.readFileSync(hookPath, 'utf8');
  });

  test('file exists', () => {
    expect(fs.existsSync(hookPath)).toBe(true);
  });

  test('exports a default async function', () => {
    expect(content).toContain('export default async function');
  });

  test('function accepts a context parameter', () => {
    expect(content).toMatch(/async function\s+\w+\s*\(\s*context\s*\)/);
  });

  test('checks for browser-act tool name', () => {
    expect(content).toContain("'browser-act'");
  });

  test('checks for browser-reason tool name', () => {
    expect(content).toContain("'browser-reason'");
  });

  test('detects crash keywords in output', () => {
    expect(content).toContain("'crash'");
  });

  test('detects error status in output', () => {
    expect(content).toContain('"status":"error"');
  });

  test('injects system diagnostic hint on error', () => {
    expect(content).toContain('[System injected via hook]');
    expect(content).toContain('browser-reason');
  });

  test('returns context (does not swallow it)', () => {
    expect(content).toContain('return context');
  });

  test('handles null/undefined context gracefully', () => {
    expect(content).toContain('!context');
  });
});
