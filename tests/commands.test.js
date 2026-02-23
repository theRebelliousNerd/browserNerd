/**
 * Custom Commands Validation Tests
 * 
 * Validates that all custom slash commands follow the Gemini CLI
 * TOML command format and contain valid prompt templates.
 */
const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(__dirname, '..');
const COMMANDS_DIR = path.join(ROOT, 'commands');

// Discover all command groups and their .toml files
const commandGroups = fs.readdirSync(COMMANDS_DIR).filter(d =>
  fs.statSync(path.join(COMMANDS_DIR, d)).isDirectory()
);

const allCommands = [];
commandGroups.forEach(group => {
  const groupDir = path.join(COMMANDS_DIR, group);
  const tomlFiles = fs.readdirSync(groupDir).filter(f => f.endsWith('.toml'));
  tomlFiles.forEach(f => {
    allCommands.push({ group, file: f, fullPath: path.join(groupDir, f) });
  });
});

describe('Commands directory', () => {
  test('exists', () => {
    expect(fs.existsSync(COMMANDS_DIR)).toBe(true);
  });

  test('contains at least one command group', () => {
    expect(commandGroups.length).toBeGreaterThanOrEqual(1);
  });

  test('contains at least one .toml command file', () => {
    expect(allCommands.length).toBeGreaterThanOrEqual(1);
  });
});

describe.each(allCommands)('Command: /$group:$file', ({ group, file, fullPath }) => {
  let content;

  beforeAll(() => {
    content = fs.readFileSync(fullPath, 'utf8');
  });

  test('is a valid .toml file with a prompt field', () => {
    expect(content).toContain('prompt');
    expect(content).toContain('=');
  });

  test('prompt field contains a non-empty string', () => {
    const match = content.match(/prompt\s*=\s*"""([\s\S]*?)"""/);
    if (!match) {
      // Single-line prompt
      const singleMatch = content.match(/prompt\s*=\s*"(.+)"/);
      expect(singleMatch).not.toBeNull();
      expect(singleMatch[1].length).toBeGreaterThan(5);
    } else {
      expect(match[1].trim().length).toBeGreaterThan(5);
    }
  });

  test('command file name uses kebab-case', () => {
    const name = file.replace('.toml', '');
    expect(name).toMatch(/^[a-z][a-z0-9-]*$/);
  });
});
