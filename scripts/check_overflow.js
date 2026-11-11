#!/usr/bin/env node

/**
 * Automated horizontal overflow and dev-banner check using headless Chromium.
 * Verifies that:
 * 1. document.documentElement.scrollWidth <= window.innerWidth at 320, 360, 390, and 768px
 *    across all required states: empty, streaming, complete, partial, error, huge.
 * 2. Dev preview banner is strictly hidden (display: none) when dev=1 is not present in URL,
 *    and only active when dev=1 is present.
 */

const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const { spawn } = require('node:child_process');

function findChromePath() {
  if (process.env.CHROME_BIN && fs.existsSync(process.env.CHROME_BIN)) {
    return process.env.CHROME_BIN;
  }
  if (process.env.CHROME_PATH && fs.existsSync(process.env.CHROME_PATH)) {
    return process.env.CHROME_PATH;
  }
  try {
    const { execSync } = require('node:child_process');
    const which = execSync('which google-chrome || which google-chrome-stable || which chromium || which chromium-browser || which chrome', {
      encoding: 'utf-8',
      stdio: ['ignore', 'pipe', 'ignore']
    }).trim();
    if (which && fs.existsSync(which)) return which;
  } catch {}

  // Check PATH entries directly
  if (process.env.PATH) {
    const paths = process.env.PATH.split(path.delimiter);
    const binaries = ['google-chrome', 'google-chrome-stable', 'chromium', 'chromium-browser', 'chrome', 'chrome.exe'];
    for (const p of paths) {
      for (const b of binaries) {
        const full = path.join(p, b);
        if (fs.existsSync(full)) {
          try {
            if (fs.statSync(full).isFile()) return full;
          } catch {}
        }
      }
    }
  }

  const candidates = [
    'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
    'C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe',
    'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe',
    '/usr/bin/google-chrome',
    '/usr/bin/google-chrome-stable',
    '/usr/bin/chromium',
    '/usr/bin/chromium-browser',
    '/usr/local/bin/chrome',
    '/snap/bin/chromium',
  ];
  for (const c of candidates) {
    if (fs.existsSync(c)) return c;
  }
  throw new Error('No Chromium or Chrome binary found');
}

const PORT = 8094;
const DEBUG_PORT = 9226;
const ASSETS_DIR = path.resolve(__dirname, '..', 'internal', 'web', 'assets');

// 1. Static file server
const server = http.createServer((req, res) => {
  let reqPath = req.url.split('?')[0];
  if (reqPath === '/' || reqPath === '') reqPath = '/index.html';
  const safePath = path.normalize(reqPath).replace(/^(\.\.[\/\\])+/, '');
  const filePath = path.join(ASSETS_DIR, safePath);

  if (fs.existsSync(filePath) && fs.statSync(filePath).isFile()) {
    const ext = path.extname(filePath);
    const contentTypes = {
      '.html': 'text/html; charset=utf-8',
      '.css': 'text/css; charset=utf-8',
      '.js': 'application/javascript; charset=utf-8',
      '.json': 'application/json',
      '.svg': 'image/svg+xml',
      '.woff2': 'font/woff2',
    };
    res.writeHead(200, { 'Content-Type': contentTypes[ext] || 'text/plain' });
    fs.createReadStream(filePath).pipe(res);
  } else {
    res.writeHead(404);
    res.end('Not Found: ' + req.url);
  }
});

async function runChecks() {
  console.log(`[check_overflow] Node: ${process.version}, Platform: ${process.platform}`);
  console.log(`[check_overflow] env.CHROME_BIN: ${process.env.CHROME_BIN || '<unset>'}`);
  console.log(`[check_overflow] env.CHROME_PATH: ${process.env.CHROME_PATH || '<unset>'}`);
  const chromePath = findChromePath();
  console.log(`[check_overflow] Using browser: ${chromePath}`);

  await new Promise(r => server.listen(PORT, '127.0.0.1', r));
  console.log(`[check_overflow] Assets server running at http://127.0.0.1:${PORT}`);

  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'chrome-chk-'));
  const chrome = spawn(chromePath, [
    '--headless=new',
    `--remote-debugging-port=${DEBUG_PORT}`,
    '--remote-debugging-address=127.0.0.1',
    `--user-data-dir=${tmpDir}`,
    '--no-sandbox',
    '--disable-setuid-sandbox',
    '--disable-dev-shm-usage',
    '--disable-gpu',
    '--no-first-run',
    '--no-default-browser-check',
    '--hide-scrollbars',
    'about:blank',
  ]);

  let chromeOutput = '';
  let chromeExited = false;
  let exitCode = null;
  chrome.stdout.on('data', d => { chromeOutput += d.toString(); });
  chrome.stderr.on('data', d => { chromeOutput += d.toString(); });
  chrome.on('exit', (code) => {
    chromeExited = true;
    exitCode = code;
  });

  let ready = false;
  for (let i = 0; i < 40; i++) {
    if (chromeExited) {
      try { fs.rmSync(tmpDir, { recursive: true, force: true }); } catch {}
      throw new Error(`Chrome process exited prematurely with code ${exitCode}.\nOutput:\n${chromeOutput}`);
    }
    try {
      const res = await fetch(`http://127.0.0.1:${DEBUG_PORT}/json/version`);
      if (res.ok) {
        ready = true;
        break;
      }
    } catch {}
    await new Promise(r => setTimeout(r, 200));
  }
  if (!ready) {
    try { chrome.kill(); } catch {}
    try { fs.rmSync(tmpDir, { recursive: true, force: true }); } catch {}
    throw new Error(`Chrome DevTools failed to become ready on port ${DEBUG_PORT} within 8s.\nOutput:\n${chromeOutput}`);
  }

  const states = ['empty', 'streaming', 'complete', 'partial', 'error', 'huge'];
  const widths = [320, 360, 390, 768];
  const height = 800;

  let failures = [];
  let testCount = 0;

  // Helper to run CDP session
  async function testPage(url, width, callback) {
    const createRes = await fetch(`http://127.0.0.1:${DEBUG_PORT}/json/new?about:blank`, { method: 'PUT' });
    const tab = await createRes.json();
    const ws = new WebSocket(tab.webSocketDebuggerUrl);

    let msgId = 1;
    function sendCommand(method, params = {}) {
      return new Promise((resolve) => {
        const id = msgId++;
        const handler = (event) => {
          const data = JSON.parse(event.data);
          if (data.id === id) {
            ws.removeEventListener('message', handler);
            resolve(data.result);
          }
        };
        ws.addEventListener('message', handler);
        ws.send(JSON.stringify({ id, method, params }));
      });
    }

    await new Promise(r => ws.addEventListener('open', r));

    await sendCommand('Page.enable');
    await sendCommand('Runtime.enable');

    // Override viewport metrics before navigation
    await sendCommand('Emulation.setDeviceMetricsOverride', {
      width,
      height,
      deviceScaleFactor: 1,
      mobile: width <= 768,
    });

    // Navigate to target url
    await sendCommand('Page.navigate', { url });

    // Wait for DOM ready & execution
    await new Promise(r => setTimeout(r, 650));

    try {
      await callback(sendCommand);
    } finally {
      ws.close();
      await fetch(`http://127.0.0.1:${DEBUG_PORT}/json/close/${tab.id}`).catch(() => {});
    }
  }

  console.log('\n--- 1. Testing Dev Preview Banner Visibility ---');
  // Dev banner without ?dev=1
  await testPage(`http://127.0.0.1:${PORT}/index.html?state=complete`, 1280, async (sendCommand) => {
    testCount++;
    const res = await sendCommand('Runtime.evaluate', {
      expression: 'window.getComputedStyle(document.querySelector(".dev-banner")).display',
      returnByValue: true,
    });
    const display = res.result.value;
    if (display !== 'none') {
      failures.push(`Dev banner visible without ?dev=1: display=${display}`);
      console.error(`FAIL: Dev banner display without ?dev=1 is ${display} (expected 'none')`);
    } else {
      console.log(`PASS: Dev banner hidden without ?dev=1 (display: 'none')`);
    }
  });

  // Dev banner with ?dev=1
  await testPage(`http://127.0.0.1:${PORT}/index.html?state=complete&dev=1`, 1280, async (sendCommand) => {
    testCount++;
    const res = await sendCommand('Runtime.evaluate', {
      expression: 'window.getComputedStyle(document.querySelector(".dev-banner")).display',
      returnByValue: true,
    });
    const display = res.result.value;
    if (display !== 'flex') {
      failures.push(`Dev banner not flex with ?dev=1: display=${display}`);
      console.error(`FAIL: Dev banner display with ?dev=1 is ${display} (expected 'flex')`);
    } else {
      console.log(`PASS: Dev banner displayed when ?dev=1 is present (display: 'flex')`);
    }
  });

  console.log('\n--- 2. Testing Horizontal Overflow across States and Viewports ---');
  for (const state of states) {
    for (const w of widths) {
      testCount++;
      const targetUrl = `http://127.0.0.1:${PORT}/index.html?state=${state}`;
      await testPage(targetUrl, w, async (sendCommand) => {
        const evalRes = await sendCommand('Runtime.evaluate', {
          expression: `({
            scrollWidth: document.documentElement.scrollWidth,
            innerWidth: window.innerWidth,
            bodyScrollWidth: document.body.scrollWidth
          })`,
          returnByValue: true,
        });

        const { scrollWidth, innerWidth, bodyScrollWidth } = evalRes.result.value;
        const maxScroll = Math.max(scrollWidth, bodyScrollWidth);

        if (maxScroll > innerWidth) {
          const diff = maxScroll - innerWidth;
          failures.push(`State '${state}' at ${w}px overflows horizontally by ${diff}px (scrollWidth=${maxScroll}, innerWidth=${innerWidth})`);
          console.error(`FAIL: State '${state}' at ${w}px: scrollWidth (${maxScroll}) > innerWidth (${innerWidth})`);
        } else {
          console.log(`PASS: State '${state}' at ${w}px: scrollWidth=${maxScroll} <= innerWidth=${innerWidth}`);
        }
      });
    }
  }

  // Teardown
  try {
    chrome.kill();
  } catch {}
  try {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  } catch {}
  server.close();

  console.log(`\n========================================`);
  console.log(`Total checks run: ${testCount}`);
  if (failures.length > 0) {
    console.error(`FAILED (${failures.length} issues):`);
    for (const f of failures) {
      console.error(` - ${f}`);
    }
    process.exit(1);
  } else {
    console.log(`ALL OVERFLOW & BANNER CHECKS PASSED cleanly!`);
    process.exit(0);
  }
}

runChecks().catch(err => {
  console.error('[check_overflow] Unhandled error:', err);
  process.exit(1);
});
