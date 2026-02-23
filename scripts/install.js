const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');

console.log('Preparing BrowserNERD MCP server...');

const os = process.platform;
const ext = os === 'win32' ? '.exe' : '';
const arch = process.arch;
const targetPath = path.join(__dirname, '../bin', `browsernerd${ext}`);

// Check if a pre-compiled release binary already exists for this architecture
// (e.g. downloaded from GitHub releases instead of compiling from source)
const prebuiltName = os === 'win32' 
  ? `browsernerd-windows-${arch}.exe` 
  : `browsernerd-${os}-${arch}`;
  
const prebuiltPath = path.join(__dirname, '../bin', prebuiltName);

try {
  // Ensure the bin directory exists
  if (!fs.existsSync(path.join(__dirname, '../bin'))) {
    fs.mkdirSync(path.join(__dirname, '../bin'));
  }

  // If a prebuilt binary exists, rename it to the target path and skip Go compilation
  if (fs.existsSync(prebuiltPath)) {
    console.log(`Found prebuilt binary for ${os}-${arch}. Skipping compilation.`);
    fs.renameSync(prebuiltPath, targetPath);
    if (os !== 'win32') {
      execSync(`chmod +x "${targetPath}"`);
    }
    console.log('Preparation successful!');
    process.exit(0);
  }

  // Otherwise, compile from source (requires Go)
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
