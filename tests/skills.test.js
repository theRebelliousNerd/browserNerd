/**
 * Agent Skills Validation Tests
 * 
 * Validates that all Agent Skills follow the Gemini CLI SKILL.md format
 * and contain the required frontmatter and instructional content.
 */
const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(__dirname, '..');
const SKILLS_DIR = path.join(ROOT, 'skills');

// Discover all skills dynamically
const skillDirs = fs.readdirSync(SKILLS_DIR).filter(d =>
  fs.statSync(path.join(SKILLS_DIR, d)).isDirectory()
);

describe('Skills directory', () => {
  test('exists', () => {
    expect(fs.existsSync(SKILLS_DIR)).toBe(true);
  });

  test('contains at least one skill', () => {
    expect(skillDirs.length).toBeGreaterThanOrEqual(1);
  });
});

describe.each(skillDirs)('Skill: %s', (skillName) => {
  const skillDir = path.join(SKILLS_DIR, skillName);
  const skillFile = path.join(skillDir, 'SKILL.md');
  let content;

  beforeAll(() => {
    if (fs.existsSync(skillFile)) {
      content = fs.readFileSync(skillFile, 'utf8');
    }
  });

  test('has a SKILL.md file', () => {
    expect(fs.existsSync(skillFile)).toBe(true);
  });

  test('SKILL.md starts with YAML frontmatter (---)', () => {
    expect(content).toBeDefined();
    expect(content.trimStart().startsWith('---')).toBe(true);
  });

  test('frontmatter contains a name field', () => {
    const frontmatter = content.split('---')[1];
    expect(frontmatter).toContain('name:');
  });

  test('frontmatter contains a description field', () => {
    const frontmatter = content.split('---')[1];
    expect(frontmatter).toContain('description:');
  });

  test('frontmatter name matches directory name', () => {
    const frontmatter = content.split('---')[1];
    const nameMatch = frontmatter.match(/name:\s*(.+)/);
    expect(nameMatch).not.toBeNull();
    expect(nameMatch[1].trim()).toBe(skillName);
  });

  test('body content is at least 100 characters long', () => {
    const parts = content.split('---');
    // parts[0] is empty, parts[1] is frontmatter, parts[2]+ is body
    const body = parts.slice(2).join('---').trim();
    expect(body.length).toBeGreaterThanOrEqual(100);
  });

  test('body references at least one BrowserNERD tool', () => {
    const body = content.split('---').slice(2).join('---');
    const tools = ['browser-observe', 'browser-act', 'browser-reason', 'browser-mangle', 'launch-browser'];
    const hasAtLeastOne = tools.some(t => body.includes(t));
    expect(hasAtLeastOne).toBe(true);
  });
});
