// Generates a self-contained, offline-compatible HTML report of the current results.

export function generateOfflineReport(snapshot, options = {}) {
  const isAnonymized = options.anonymized || false;
  const results = snapshot.results || [];
  const unreachable = snapshot.unreachable || [];
  const summary = snapshot.summary || {};
  const goal = summary.goal || 10;
  const completeCount = summary.complete_count || 0;
  const totalCount = summary.total_students || (results.length + unreachable.length);

  // Build rows HTML
  const rowsHtml = results.map(r => {
    const name = isAnonymized ? (r.pseudonym || 'Learner-XXXX') : (r.display_name || 'Anonymous');
    const count = r.counted_badges || 0;
    const isComplete = r.status === 'complete' || count >= goal;
    const lastDate = r.last_badge_date || '—';

    // 10-segment bar
    let segments = '';
    for (let i = 0; i < 10; i++) {
      const filled = i < count;
      segments += `<span class="seg ${filled ? 'filled' : 'empty'}"></span>`;
    }
    const checkmark = isComplete ? `<span class="check-sym">✓</span>` : '';

    return `
      <tr>
        <td class="col-name">${escapeHtml(name)}</td>
        <td class="col-prog"><div class="seg-bar">${segments}</div> ${checkmark}</td>
        <td class="col-count">${count}/${goal}</td>
        <td class="col-status status-${r.status}">[${isComplete ? '✓' : '>'}] ${formatStatus(r.status)}</td>
        <td class="col-date">${escapeHtml(lastDate)}</td>
      </tr>
    `;
  }).join('');

  // Build unreachable rows HTML if any
  let unreachableHtml = '';
  if (unreachable.length > 0) {
    const unrecRows = unreachable.map(u => {
      const name = isAnonymized ? (u.pseudonym || 'Learner-XXXX') : (u.display_name || 'Anonymous');
      return `
        <tr>
          <td class="col-name">${escapeHtml(name)}</td>
          <td class="col-status status-unreachable">[!] ${escapeHtml(u.reason || formatStatus(u.status))}</td>
        </tr>
      `;
    }).join('');

    unreachableHtml = `
      <section class="unreachable-section">
        <h2>Couldn't be Checked (${unreachable.length})</h2>
        <p class="notice">These profiles could not be read. They are not counted as zero.</p>
        <table class="report-table">
          <thead>
            <tr><th>Learner</th><th>Status Reason</th></tr>
          </thead>
          <tbody>${unrecRows}</tbody>
        </table>
      </section>
    `;
  }

  // Distribution buckets
  const dist = computeDistribution(results, goal);

  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Google Skills Badgeboard Report</title>
<style>
  :root {
    --bg: #F6F1E7;
    --ink: #1A1814;
    --muted: #635F57;
    --border: #DDD6C9;
    --accent: #BA3E18;
    --card-bg: #FFFFFF;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg: #14120F;
      --ink: #EDE6D8;
      --muted: #9E9689;
      --border: #2A2621;
      --accent: #E05A2B;
      --card-bg: #1C1915;
    }
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    background: var(--bg);
    color: var(--ink);
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    line-height: 1.5;
    padding: 32px 24px;
    max-width: 960px;
    margin: 0 auto;
  }
  header {
    border-bottom: 2px solid var(--border);
    padding-bottom: 20px;
    margin-bottom: 28px;
  }
  h1 { font-size: 26px; font-weight: 700; margin-bottom: 4px; }
  .subtitle { font-size: 13px; text-transform: uppercase; letter-spacing: 0.05em; color: var(--muted); }
  .summary-lead {
    font-size: 28px;
    font-weight: 700;
    line-height: 1.25;
    margin: 20px 0;
    color: var(--accent);
  }
  .dist-strip {
    display: flex;
    gap: 8px;
    background: var(--card-bg);
    border: 1px solid var(--border);
    padding: 12px 16px;
    margin-bottom: 24px;
  }
  .dist-item { flex: 1; text-align: center; }
  .dist-val { font-size: 18px; font-weight: 700; font-family: monospace; }
  .dist-lbl { font-size: 11px; text-transform: uppercase; color: var(--muted); }
  .report-table {
    width: 100%;
    border-collapse: collapse;
    margin-top: 16px;
    font-size: 13.5px;
  }
  .report-table th, .report-table td {
    border-bottom: 1px solid var(--border);
    padding: 10px 12px;
    text-align: left;
  }
  .report-table th {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--muted);
  }
  .seg-bar {
    display: inline-flex;
    gap: 3px;
    vertical-align: middle;
  }
  .seg {
    display: inline-block;
    width: 9px;
    height: 14px;
    border: 1px solid var(--accent);
  }
  .seg.filled { background: var(--accent); }
  .seg.empty { background: transparent; opacity: 0.35; }
  .check-sym { color: #1E8E3E; font-weight: bold; margin-left: 4px; }
  .notice { font-size: 13px; font-style: italic; color: var(--muted); margin: 6px 0 12px; }
  .unreachable-section { margin-top: 40px; }
  footer {
    border-top: 1px solid var(--border);
    margin-top: 40px;
    padding-top: 16px;
    font-size: 12px;
    color: var(--muted);
  }
  @media print {
    body { background: #FFFFFF !important; color: #000000 !important; padding: 0; }
    .seg.filled { background: #000000 !important; border-color: #000000 !important; }
    .seg.empty { border-color: #999999 !important; }
    .summary-lead { color: #000000 !important; }
  }
</style>
</head>
<body>
  <header>
    <h1>Google Skills Badgeboard</h1>
    <div class="subtitle">GDG Learn With Communities • Progress Report</div>
  </header>

  <main>
    <div class="summary-lead">${completeCount} of ${totalCount} learners have completed all ${goal} badges.</div>

    <div class="dist-strip">
      <div class="dist-item"><div class="dist-val">${dist.zero}</div><div class="dist-lbl">0 badges</div></div>
      <div class="dist-item"><div class="dist-val">${dist.oneToThree}</div><div class="dist-lbl">1–3 badges</div></div>
      <div class="dist-item"><div class="dist-val">${dist.fourToSix}</div><div class="dist-lbl">4–6 badges</div></div>
      <div class="dist-item"><div class="dist-val">${dist.sevenToNine}</div><div class="dist-lbl">7–9 badges</div></div>
      <div class="dist-item"><div class="dist-val">${dist.ten}</div><div class="dist-lbl">10 badges</div></div>
    </div>

    <table class="report-table">
      <thead>
        <tr>
          <th>Learner</th>
          <th>Progress</th>
          <th>Count</th>
          <th>Status</th>
          <th>Last Earned</th>
        </tr>
      </thead>
      <tbody>
        ${rowsHtml}
      </tbody>
    </table>

    ${unreachableHtml}
  </main>

  <footer>
    <p>How counting works: Any verified Google Skills badge counts towards campaign progress. The campaign completion goal is ${goal} badges. Private profiles or network errors are marked as unread and are never counted as zero.</p>
  </footer>
</body>
</html>`;
}

function escapeHtml(str) {
  if (!str) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function formatStatus(status) {
  switch (status) {
    case 'complete': return 'Complete';
    case 'in_progress': return 'In progress';
    case 'no_badges': return 'No badges';
    case 'private_or_not_found': return 'Private / 404';
    case 'network_error': return 'Network error';
    case 'rate_limited': return 'Rate limited';
    case 'parse_error': return 'Parse error';
    default: return status || 'Unknown';
  }
}

export function computeDistribution(results = [], goal = 10) {
  let zero = 0;
  let oneToThree = 0;
  let fourToSix = 0;
  let sevenToNine = 0;
  let ten = 0;

  for (const r of results) {
    const c = r.counted_badges || 0;
    if (c === 0) zero++;
    else if (c >= 1 && c <= 3) oneToThree++;
    else if (c >= 4 && c <= 6) fourToSix++;
    else if (c >= 7 && c <= 9) sevenToNine++;
    else if (c >= goal) ten++;
  }

  return { zero, oneToThree, fourToSix, sevenToNine, ten };
}
