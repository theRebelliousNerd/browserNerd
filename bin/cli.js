#!/usr/bin/env node
const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');

const ext = process.platform === 'win32' ? '.exe' : '';
const binPath = path.join(__dirname, `browsernerd${ext}`);

if (!fs.existsSync(binPath)) {
    console.error(`\n🚨 BrowserNERD binary not found at: ${binPath}`);
    console.error('Please make sure you have Go installed (1.21+) and run `npm install` inside the browsernerd extension directory to compile the binary.\n');
    process.exit(1);
}

// Pass all arguments down to the Go binary
const args = process.argv.slice(2);
const child = spawn(binPath, args, { stdio: 'inherit' });

child.on('error', (err) => {
    console.error('Failed to start BrowserNERD process:', err);
    process.exit(1);
});

child.on('exit', (code) => {
    process.exit(code || 0);
});
