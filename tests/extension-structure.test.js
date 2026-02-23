/**
 * Extension Structure Validation Tests
 * 
 * Validates that all files required by the Gemini CLI extension gallery
 * are present, correctly formatted, and internally consistent.
 */
const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(__dirname, '..');

describe('gemini-extension.json', () => {
  let manifest;

  beforeAll(() => {
    const raw = fs.readFileSync(path.join(ROOT, 'gemini-extension.json'), 'utf8');
    manifest = JSON.parse(raw);
  });

  test('exists at repository root', () => {
    expect(fs.existsSync(path.join(ROOT, 'gemini-extension.json'))).toBe(true);
  });

  test('is valid JSON', () => {
    expect(manifest).toBeDefined();
    expect(typeof manifest).toBe('object');
  });

  test('has a name field', () => {
    expect(manifest.name).toBeDefined();
    expect(typeof manifest.name).toBe('string');
    expect(manifest.name.length).toBeGreaterThan(0);
  });

  test('has a version field matching semver pattern', () => {
    expect(manifest.version).toBeDefined();
    expect(manifest.version).toMatch(/^\d+\.\d+\.\d+/);
  });

  test('has a description field', () => {
    expect(manifest.description).toBeDefined();
    expect(manifest.description.length).toBeGreaterThan(10);
  });

  test('declares at least one MCP server', () => {
    expect(manifest.mcpServers).toBeDefined();
    const serverNames = Object.keys(manifest.mcpServers);
    expect(serverNames.length).toBeGreaterThanOrEqual(1);
  });

  test('MCP server entry has a command', () => {
    const server = manifest.mcpServers[Object.keys(manifest.mcpServers)[0]];
    expect(server.command).toBeDefined();
    expect(typeof server.command).toBe('string');
  });

  test('MCP server entry has args array', () => {
    const server = manifest.mcpServers[Object.keys(manifest.mcpServers)[0]];
    expect(server.args).toBeDefined();
    expect(Array.isArray(server.args)).toBe(true);
  });

  test('MCP server uses ${extensionPath} template variables', () => {
    const raw = fs.readFileSync(path.join(ROOT, 'gemini-extension.json'), 'utf8');
    expect(raw).toContain('${extensionPath}');
    expect(raw).toContain('${/}');
  });

  test('declares a contextFileName pointing to an existing file', () => {
    expect(manifest.contextFileName).toBeDefined();
    const contextPath = path.join(ROOT, manifest.contextFileName);
    expect(fs.existsSync(contextPath)).toBe(true);
  });
});

describe('GEMINI.md context file', () => {
  let content;

  beforeAll(() => {
    content = fs.readFileSync(path.join(ROOT, 'GEMINI.md'), 'utf8');
  });

  test('exists at repository root', () => {
    expect(fs.existsSync(path.join(ROOT, 'GEMINI.md'))).toBe(true);
  });

  test('is non-empty', () => {
    expect(content.length).toBeGreaterThan(100);
  });

  test('contains tool usage instructions for browser-observe', () => {
    expect(content).toContain('browser-observe');
  });

  test('contains tool usage instructions for browser-act', () => {
    expect(content).toContain('browser-act');
  });

  test('contains tool usage instructions for browser-reason', () => {
    expect(content).toContain('browser-reason');
  });

  test('contains JSON examples for browser-act operations', () => {
    expect(content).toContain('"operations"');
    expect(content).toContain('"type"');
    expect(content).toContain('"ref"');
  });

  test('contains fill_form batch operation example', () => {
    expect(content).toContain('fill_form');
    expect(content).toContain('"fills"');
  });

  test('instructs agent to never dump raw HTML', () => {
    expect(content.toLowerCase()).toContain('never');
    expect(content.toLowerCase()).toContain('raw html');
  });

  test('instructs agent to use refs from browser-observe', () => {
    expect(content.toLowerCase()).toContain('refs');
  });
});

describe('package.json', () => {
  let pkg;

  beforeAll(() => {
    const raw = fs.readFileSync(path.join(ROOT, 'package.json'), 'utf8');
    pkg = JSON.parse(raw);
  });

  test('contains gemini-cli-extension keyword for gallery discovery', () => {
    expect(pkg.keywords).toContain('gemini-cli-extension');
  });

  test('has a bin entry pointing to cli.js', () => {
    expect(pkg.bin).toBeDefined();
    expect(pkg.bin.browsernerd).toContain('cli.js');
  });

  test('has a postinstall script', () => {
    expect(pkg.scripts.postinstall).toBeDefined();
  });

  test('has a build script', () => {
    expect(pkg.scripts.build).toBeDefined();
  });

  test('has an Apache-2.0 license', () => {
    expect(pkg.license).toBe('Apache-2.0');
  });
});
