const assert = require('assert');
const { sanitizeCSVCell, generateCSV } = require('./app.js');

console.log('Testing sanitizeCSVCell formula injection neutralization...');

// 1. Formula injection triggers (=, +, -, @, \t, \r)
assert.strictEqual(sanitizeCSVCell('=cmd|calc!A0'), "'=cmd|calc!A0");
assert.strictEqual(sanitizeCSVCell('+https://skills.google'), "'+https://skills.google");
assert.strictEqual(sanitizeCSVCell('-12345'), "'-12345");
assert.strictEqual(sanitizeCSVCell('@Admin'), "'@Admin");
assert.strictEqual(sanitizeCSVCell('\tTabLeading'), "'\tTabLeading");
assert.strictEqual(sanitizeCSVCell('\rReturnLeading'), '"\'\rReturnLeading"');

// 2. Safe strings
assert.strictEqual(sanitizeCSVCell('Normal Student'), 'Normal Student');
assert.strictEqual(sanitizeCSVCell('10'), '10');
assert.strictEqual(sanitizeCSVCell(''), '');
assert.strictEqual(sanitizeCSVCell(null), '');
assert.strictEqual(sanitizeCSVCell(undefined), '');

// 3. Quoting and escaping special CSV characters
assert.strictEqual(sanitizeCSVCell('Doe, Jane'), '"Doe, Jane"');
assert.strictEqual(sanitizeCSVCell('He said "Hello"'), '"He said ""Hello"""');
assert.strictEqual(sanitizeCSVCell('=SUM(A1, B1)'), '"\'=SUM(A1, B1)"');

console.log('Testing generateCSV table output...');

// 4. Test table generation
const testRecords = [
  {
    student: {
      name: '=cmd|calc!A0',
      profile_url: '+https://skills.google/public_profiles/11111111-2222-3333-4444-555555555555',
      uuid: '-11111111-2222-3333-4444-555555555555',
    },
    display_name: '=cmd|calc!A0',
    status: 'complete',
    counted_badges: 10,
    total_badges: 10,
    last_badge_date: '2026-11-04',
    league: '\tTabLeague',
    points: 400,
    error: '@ErrorNotice',
  }
];

const csvOut = generateCSV(testRecords, 10);
const lines = csvOut.split('\r\n');

assert.strictEqual(lines.length, 2, 'expected header + 1 data line');
assert(lines[0].startsWith('Name,'), 'header must start with Name');
assert(lines[1].includes("'=cmd|calc!A0"), 'formula cell must be neutralized with leading single quote');
assert(lines[1].includes("'+https://skills.google"), 'plus sign must be neutralized');
assert(lines[1].includes("'-11111111"), 'minus sign must be neutralized');
assert(lines[1].includes("'\tTabLeague"), 'tab character must be neutralized');
assert(lines[1].includes("'@ErrorNotice"), '@ symbol must be neutralized');

console.log('All CSV Export tests passed successfully (PASS)!');
