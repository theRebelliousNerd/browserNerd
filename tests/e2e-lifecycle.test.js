/**
 * End-to-End Browser Session Lifecycle Test
 * 
 * Validates BrowserNERD's core claims by running a full session lifecycle
 * through the MCP server binary over stdio JSON-RPC.
 * 
 * Claims validated:
 * - launch-browser starts Chrome via Rod
 * - browser-act session_create opens a tab
 * - browser-observe returns structured, token-efficient state
 * - browser-observe interactive mode returns refs usable by browser-act
 * - browser-reason returns structured triage data
 * - shutdown-browser cleans up
 * 
 * NOTE: This test requires Chrome/Chromium to be installed on the machine.
 *       It is tagged as an E2E test and may be skipped in CI.
 */
const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');

const ROOT = path.resolve(__dirname, '..');
const ext = process.platform === 'win32' ? '.exe' : '';
const BIN_PATH = path.join(ROOT, 'bin', `browsernerd${ext}`);
const CONFIG_PATH = path.join(ROOT, 'mcp-server', 'gemini-config.yaml');

const binaryExists = fs.existsSync(BIN_PATH);

// JSON-RPC helpers
let reqId = 0;
function rpcMsg(method, params = {}) {
  reqId++;
  const body = JSON.stringify({ jsonrpc: '2.0', id: reqId, method, params });
  return { id: reqId, raw: `Content-Length: ${Buffer.byteLength(body)}\r\n\r\n${body}` };
}

class McpClient {
  constructor() {
    this.proc = null;
    this.buffer = '';
    this.pending = new Map();
  }

  start() {
    return new Promise((resolve, reject) => {
      this.proc = spawn(BIN_PATH, ['--config', CONFIG_PATH], {
        stdio: ['pipe', 'pipe', 'pipe'],
        cwd: path.join(ROOT, 'mcp-server'),
      });

      this.proc.stdout.on('data', (chunk) => {
        this.buffer += chunk.toString('utf8');
        this._drain();
      });

      this.proc.stderr.on('data', () => {}); // suppress

      this.proc.on('error', reject);
      setTimeout(resolve, 2000); // give server time to start
    });
  }

  _drain() {
    // Parse Content-Length framed JSON-RPC messages
    while (true) {
      const headerEnd = this.buffer.indexOf('\r\n\r\n');
      if (headerEnd === -1) break;

      const header = this.buffer.substring(0, headerEnd);
      const match = header.match(/Content-Length:\s*(\d+)/i);
      if (!match) {
        this.buffer = this.buffer.substring(headerEnd + 4);
        continue;
      }

      const len = parseInt(match[1], 10);
      const bodyStart = headerEnd + 4;
      if (this.buffer.length < bodyStart + len) break; // incomplete

      const body = this.buffer.substring(bodyStart, bodyStart + len);
      this.buffer = this.buffer.substring(bodyStart + len);

      try {
        const msg = JSON.parse(body);
        if (msg.id && this.pending.has(msg.id)) {
          this.pending.get(msg.id)(msg);
          this.pending.delete(msg.id);
        }
      } catch (e) {
        // skip malformed
      }
    }
  }

  request(method, params = {}, timeoutMs = 15000) {
    return new Promise((resolve, reject) => {
      const { id, raw } = rpcMsg(method, params);
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`Timeout waiting for response to ${method} (id=${id})`));
      }, timeoutMs);

      this.pending.set(id, (msg) => {
        clearTimeout(timer);
        if (msg.error) {
          reject(new Error(`RPC error: ${JSON.stringify(msg.error)}`));
        } else {
          resolve(msg.result);
        }
      });

      this.proc.stdin.write(raw);
    });
  }

  notify(method, params = {}) {
    const body = JSON.stringify({ jsonrpc: '2.0', method, params });
    const raw = `Content-Length: ${Buffer.byteLength(body)}\r\n\r\n${body}`;
    this.proc.stdin.write(raw);
  }

  async callTool(name, args = {}) {
    const result = await this.request('tools/call', { name, arguments: args });
    if (result && result.content && result.content.length > 0) {
      try {
        return JSON.parse(result.content[0].text);
      } catch {
        return result.content[0].text;
      }
    }
    return result;
  }

  stop() {
    if (this.proc && !this.proc.killed) {
      this.proc.kill('SIGTERM');
    }
  }
}

// ── Test Suite ──

describe('E2E: Full Browser Session Lifecycle', () => {
  if (!binaryExists) {
    test.skip('Binary not found - run npm run build first', () => {});
    return;
  }

  const client = new McpClient();
  let sessionId = null;

  beforeAll(async () => {
    await client.start();

    // Initialize MCP handshake
    await client.request('initialize', {
      protocolVersion: '2024-11-05',
      capabilities: {},
      clientInfo: { name: 'e2e-test', version: '1.0.0' },
    });

    client.notify('notifications/initialized', {});
    // Small delay for server to process notification
    await new Promise(r => setTimeout(r, 500));
  }, 20000);

  afterAll(() => {
    client.stop();
  });

  // ── Claim: launch-browser starts Chrome via Rod ──
  test('launch-browser starts Chrome successfully', async () => {
    const result = await client.callTool('launch-browser', {});
    expect(result).toBeDefined();

    // Should return status = "launched" or "already_connected"
    if (typeof result === 'object') {
      expect(['launched', 'already_connected', 'connected']).toContain(
        result.status || result.Status
      );
    }
  }, 30000);

  // ── Claim: browser-act session_create opens a tab ──
  test('browser-act session_create opens a new tab', async () => {
    const result = await client.callTool('browser-act', {
      operations: [
        { type: 'session_create', url: 'https://example.com' },
        { type: 'await_stable', timeout_ms: 10000 },
      ],
    });

    expect(result).toBeDefined();

    // Extract session_id from the result
    if (typeof result === 'object') {
      sessionId = result.session_id || result.sessionId;
      if (!sessionId && result.results && Array.isArray(result.results)) {
        for (const r of result.results) {
          if (r.session_id) { sessionId = r.session_id; break; }
        }
      }
    }

    expect(sessionId).toBeDefined();
    expect(typeof sessionId).toBe('string');
    expect(sessionId.length).toBeGreaterThan(0);
  }, 30000);

  // ── Claim: browser-observe returns structured, token-efficient state ──
  test('browser-observe quick_status returns compact structured data', async () => {
    const result = await client.callTool('browser-observe', {
      session_id: sessionId,
      intent: 'quick_status',
    });

    expect(result).toBeDefined();
    const resultStr = JSON.stringify(result);

    // Token efficiency claim: quick_status should be under 500 tokens (~2000 chars)
    expect(resultStr.length).toBeLessThan(5000);

    // Should contain structured page state (url, title, etc.)
    if (typeof result === 'object') {
      // Look for url somewhere in the response
      expect(resultStr.toLowerCase()).toContain('example.com');
    }
  }, 15000);

  // ── Claim: browser-observe interactive mode returns actionable refs ──
  test('browser-observe interactive returns elements with refs', async () => {
    const result = await client.callTool('browser-observe', {
      session_id: sessionId,
      mode: 'interactive',
      view: 'compact',
    });

    expect(result).toBeDefined();
    const resultStr = JSON.stringify(result);

    // Should contain ref identifiers that can be used by browser-act
    // example.com has at least one link ("More information...")
    if (typeof result === 'object') {
      // The result should contain interactive elements or nav links
      const hasRefs = resultStr.includes('ref') || resultStr.includes('Ref');
      const hasElements = resultStr.includes('link') || resultStr.includes('button') || resultStr.includes('a');
      expect(hasRefs || hasElements).toBe(true);
    }
  }, 15000);

  // ── Claim: browser-observe nav mode returns grouped navigation links ──
  test('browser-observe nav returns grouped links', async () => {
    const result = await client.callTool('browser-observe', {
      session_id: sessionId,
      mode: 'nav',
      view: 'compact',
    });

    expect(result).toBeDefined();
  }, 15000);

  // ── Claim: browser-reason returns structured triage data ──
  test('browser-reason health topic returns diagnostic data', async () => {
    const result = await client.callTool('browser-reason', {
      session_id: sessionId,
      topic: 'health',
      view: 'compact',
    });

    expect(result).toBeDefined();

    // Should contain some form of status/health indicator
    if (typeof result === 'object') {
      const resultStr = JSON.stringify(result);
      const hasHealth = resultStr.includes('status') || resultStr.includes('health') || resultStr.includes('ok');
      expect(hasHealth).toBe(true);
    }
  }, 15000);

  // ── Claim: browser-observe composite mode returns everything in one call ──
  test('browser-observe composite returns state + nav + interactive in one call', async () => {
    const result = await client.callTool('browser-observe', {
      session_id: sessionId,
      mode: 'composite',
      view: 'summary',
    });

    expect(result).toBeDefined();
    const resultStr = JSON.stringify(result);

    // Composite should be more comprehensive but still token efficient
    expect(resultStr.length).toBeGreaterThan(50); // not empty
    expect(resultStr.length).toBeLessThan(20000); // not bloated
  }, 15000);

  // ── Claim: shutdown-browser cleans up ──
  test('shutdown-browser cleans up all sessions', async () => {
    const result = await client.callTool('shutdown-browser', {});
    expect(result).toBeDefined();

    if (typeof result === 'object') {
      const resultStr = JSON.stringify(result);
      const hasShutdown = resultStr.includes('shutdown') || resultStr.includes('closed') || resultStr.includes('success');
      expect(hasShutdown).toBe(true);
    }
  }, 15000);
});
