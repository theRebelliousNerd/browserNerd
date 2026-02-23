const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');

console.log('Building BrowserNERD MCP server...');

const os = process.platform;
const ext = os === 'win32' ? '.exe' : '';
const targetPath = path.join(__dirname, '../bin', `browsernerd${ext}`);

try {
  // Ensure the bin directory exists
  if (!fs.existsSync(path.join(__dirname, '../bin'))) {
    fs.mkdirSync(path.join(__dirname, '../bin'));
  }

  console.log(`Compiling Go binary to ${targetPath}...`);
  execSync(`go build -o "${targetPath}" ./cmd/server`, {
    cwd: path.join(__dirname, '../mcp-server'),
    stdio: 'inherit'
  });
  console.log('Build successful!');
} catch (e) {
  console.warn('\n======================================================');
  console.warn('⚠️ WARNING: BrowserNERD compilation failed or Go is not installed.');
  console.warn('To use this extension, please ensure Go 1.21+ is installed.');
  console.warn('Then manually run `go build` inside the mcp-server/ directory.');
  console.warn('======================================================\n');
}
