// Frontend unit tests for pure functions using Node.js built-in test runner (node --test)
import test from 'node:test';
import assert from 'node:assert/strict';
import { sanitizeCSVCell, escapeCSVCell, parseRosterCSV, parseCSVLine, generateCSVExport } from './csv.js';
import { computeDistribution } from './report.js';

test('sanitizeCSVCell: neutralizes formula injection', () => {
  assert.equal(sanitizeCSVCell('=1+1'), "'=1+1");
  assert.equal(sanitizeCSVCell('+cmd|/c calc'), "'+cmd|/c calc");
  assert.equal(sanitizeCSVCell('-2+3'), "'-2+3");
  assert.equal(sanitizeCSVCell('@SUM(A1:A10)'), "'@SUM(A1:A10)");
  assert.equal(sanitizeCSVCell('\tmalicious'), "'\tmalicious");
  assert.equal(sanitizeCSVCell('\rmalicious'), "'\rmalicious");

  // Normal strings unaffected
  assert.equal(sanitizeCSVCell('Corin Calder'), 'Corin Calder');
  assert.equal(sanitizeCSVCell('https://www.skills.google'), 'https://www.skills.google');
  assert.equal(sanitizeCSVCell(''), '');
  assert.equal(sanitizeCSVCell(null), '');
  assert.equal(sanitizeCSVCell(undefined), '');
});

test('escapeCSVCell: quotes cells with commas, quotes, and newlines', () => {
  assert.equal(escapeCSVCell('Simple Text'), 'Simple Text');
  assert.equal(escapeCSVCell('Text, with comma'), '"Text, with comma"');
  assert.equal(escapeCSVCell('Text "with quotes"'), '"Text ""with quotes"""');
  assert.equal(escapeCSVCell('=Formula, Dangerous'), '"\'=Formula, Dangerous"');
});

test('parseRosterCSV: parses valid profile URLs from CSV', () => {
  const csv = `Google Skills Profile URL,[INFO]
https://www.skills.google/public_profiles/11111111-2222-3333-4444-555555555555,All completed
https://www.skills.google/public_profiles/22222222-3333-4444-5555-666666666666,In progress
`;
  const result = parseRosterCSV(csv);
  assert.equal(result.urls.length, 2);
  assert.equal(result.urls[0], 'https://www.skills.google/public_profiles/11111111-2222-3333-4444-555555555555');
  assert.equal(result.urls[1], 'https://www.skills.google/public_profiles/22222222-3333-4444-5555-666666666666');
  assert.equal(result.badRows, 0);
});

test('parseRosterCSV: handles single-column CSV without headers or custom header', () => {
  const csv = `https://www.skills.google/public_profiles/11111111-2222-3333-4444-555555555555
https://www.skills.google/public_profiles/22222222-3333-4444-5555-666666666666
`;
  const result = parseRosterCSV(csv);
  assert.equal(result.urls.length, 2);
  assert.equal(result.badRows, 0);
});

test('generateCSVExport: outputs valid neutralized CSV', () => {
  const records = [
    { display_name: 'Corin Calder', status: 'complete', counted_badges: 10, goal: 10, last_badge_date: '2026-11-05' },
    { display_name: '=Dangerous Name', status: 'in_progress', counted_badges: 5, goal: 10, last_badge_date: '2026-10-20' }
  ];
  const output = generateCSVExport(records, false);
  assert.match(output, /Learner,Status,Counted Badges,Goal,Last Badge Date/);
  assert.match(output, /Corin Calder,complete,10,10,2026-11-05/);
  assert.match(output, /'=Dangerous Name,in_progress,5,10,2026-10-20/);
});

test('computeDistribution: buckets counts accurately', () => {
  const results = [
    { counted_badges: 0 },
    { counted_badges: 2 },
    { counted_badges: 3 },
    { counted_badges: 5 },
    { counted_badges: 8 },
    { counted_badges: 10 },
    { counted_badges: 12 }
  ];
  const dist = computeDistribution(results, 10);
  assert.equal(dist.zero, 1);
  assert.equal(dist.oneToThree, 2);
  assert.equal(dist.fourToSix, 1);
  assert.equal(dist.sevenToNine, 1);
  assert.equal(dist.ten, 2);
});

test('devBanner: style rules strictly hide dev banner unless .dev-active', async () => {
  const fs = await import('node:fs');
  const path = await import('node:path');
  const css = fs.readFileSync(new URL('./style.css', import.meta.url), 'utf8');

  // Verify .dev-banner is hidden by default
  assert.match(css, /\.dev-banner\s*\{[^}]*display:\s*none\s*!important/);
  // Verify .dev-banner.dev-active displays flex
  assert.match(css, /\.dev-banner\.dev-active\s*\{[^}]*display:\s*flex\s*!important/);
});

test('dropzone: accessible markup and sr-only file input', async () => {
  const fs = await import('node:fs');
  const html = fs.readFileSync(new URL('./index.html', import.meta.url), 'utf8');

  assert.match(html, /id="dropzone"[^>]*role="button"[^>]*tabindex="0"/);
  assert.match(html, /id="fileInput"[^>]*class="sr-only"/);
  assert.match(html, /id="accessCodeModal"/);
});
