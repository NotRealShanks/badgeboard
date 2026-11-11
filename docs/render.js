// Pure DOM-builder rendering functions for badgeboard.
// ZERO innerHTML is used — all nodes are constructed safely via document.createElement, textContent, and attributes.

/**
 * Creates the large Fraunces summary headline.
 */
export function renderSummarySentence(completeCount, totalCount, goal = 10, comparisonDiff = null, isBlockedOrAllUnreachable = false) {
  const container = document.createElement('div');
  container.className = 'summary-sentence-box';

  const h2 = document.createElement('h2');
  h2.className = 'summary-sentence';
  if (isBlockedOrAllUnreachable) {
    h2.textContent = 'The server could not reach Google Skills from here.';
  } else {
    h2.textContent = `${completeCount} of ${totalCount} learners have completed all ${goal} badges.`;
  }

  if (!isBlockedOrAllUnreachable && comparisonDiff !== null && comparisonDiff !== undefined) {
    const span = document.createElement('span');
    span.className = 'summary-comparison';
    if (comparisonDiff > 0) {
      span.textContent = ` (+${comparisonDiff} completed since last run)`;
      span.style.color = 'var(--accent)';
      span.style.fontSize = '0.65em';
      span.style.display = 'inline-block';
      span.style.marginLeft = '8px';
      h2.appendChild(span);
    } else if (comparisonDiff === 0) {
      span.textContent = ` (no change since last run)`;
      span.style.color = 'var(--ink-secondary)';
      span.style.fontSize = '0.65em';
      span.style.display = 'inline-block';
      span.style.marginLeft = '8px';
      h2.appendChild(span);
    }
  }

  container.appendChild(h2);
  return container;
}

/**
 * Creates the 5-bucket distribution bar (0, 1–3, 4–6, 7–9, 10).
 */
export function renderDistributionBar(dist, total) {
  const section = document.createElement('div');
  section.className = 'distribution-section';
  section.setAttribute('aria-label', 'Progress Distribution');

  const title = document.createElement('div');
  title.className = 'dist-title';
  title.textContent = 'Badge Distribution Across Roster';
  section.appendChild(title);

  const track = document.createElement('div');
  track.className = 'dist-bar-track';

  const buckets = [
    { label: '0', count: dist.zero, cls: 'dist-bucket-0' },
    { label: '1–3', count: dist.oneToThree, cls: 'dist-bucket-1-3' },
    { label: '4–6', count: dist.fourToSix, cls: 'dist-bucket-4-6' },
    { label: '7–9', count: dist.sevenToNine, cls: 'dist-bucket-7-9' },
    { label: '10', count: dist.ten, cls: 'dist-bucket-10' }
  ];

  for (const b of buckets) {
    const widthPct = total > 0 ? (b.count / total) * 100 : 20;
    const bucketEl = document.createElement('div');
    bucketEl.className = `dist-bucket ${b.cls}`;
    bucketEl.style.flex = `${Math.max(b.count, 0.5)}`;
    bucketEl.textContent = `${b.label}: ${b.count}`;
    bucketEl.setAttribute('title', `${b.label} badges: ${b.count} participants (${total > 0 ? Math.round((b.count / total) * 100) : 0}%)`);
    track.appendChild(bucketEl);
  }

  section.appendChild(track);
  return section;
}

/**
 * Builds the signature 10-segment progress bar.
 */
export function render10SegmentBar(count, goal = 10, isComplete = false) {
  const wrapper = document.createElement('div');
  wrapper.className = 'seg-bar-container';

  const bar = document.createElement('div');
  bar.className = 'seg-bar-10';
  bar.setAttribute('role', 'progressbar');
  bar.setAttribute('aria-valuenow', String(count));
  bar.setAttribute('aria-valuemin', '0');
  bar.setAttribute('aria-valuemax', String(goal));
  bar.setAttribute('aria-label', `${count} of ${goal} badges completed`);

  for (let i = 0; i < 10; i++) {
    const block = document.createElement('span');
    block.className = `seg-block ${i < count ? 'filled' : 'empty'}`;
    bar.appendChild(block);
  }

  wrapper.appendChild(bar);

  if (isComplete || count >= goal) {
    const check = document.createElement('span');
    check.className = 'seg-check';
    check.textContent = '✓';
    check.setAttribute('aria-hidden', 'true');
    wrapper.appendChild(check);
  }

  return wrapper;
}

/**
 * Builds status indicator with Symbol + Word.
 */
export function renderStatusPill(status) {
  const pill = document.createElement('span');
  pill.className = `status-pill status-${status}`;

  let symbol = '[>]';
  let label = 'In progress';

  switch (status) {
    case 'complete':
      symbol = '[✓]';
      label = 'Complete';
      break;
    case 'in_progress':
      symbol = '[>]';
      label = 'In progress';
      break;
    case 'no_badges':
      symbol = '[-]';
      label = 'No badges';
      break;
    case 'private_or_not_found':
      symbol = '[?]';
      label = 'Private / 404';
      break;
    case 'rate_limited':
      symbol = '[!]';
      label = 'Rate limited';
      break;
    case 'network_error':
      symbol = '[!]';
      label = 'Network error';
      break;
    case 'parse_error':
      symbol = '[!]';
      label = 'Parse error';
      break;
  }

  pill.textContent = `${symbol} ${label}`;
  return pill;
}

/**
 * Renders the roster table with responsive layout and expandable badge rows.
 */
export function renderRosterTable(results, options = {}) {
  const isAnonymized = options.anonymized || false;
  const showBadges = options.showBadges !== undefined ? options.showBadges : true;
  const goal = options.goal || 10;

  const container = document.createElement('div');
  container.className = 'roster-table-container';

  const table = document.createElement('table');
  table.className = 'roster-table';

  // Table Header
  const thead = document.createElement('thead');
  const headRow = document.createElement('tr');

  const headers = ['Learner', 'Progress', 'Count', 'Status', 'Last Earned'];
  for (const h of headers) {
    const th = document.createElement('th');
    th.setAttribute('scope', 'col');
    th.textContent = h;
    headRow.appendChild(th);
  }
  thead.appendChild(headRow);
  table.appendChild(thead);

  // Table Body
  const tbody = document.createElement('tbody');

  for (const r of results) {
    const name = isAnonymized ? (r.pseudonym || 'Learner-XXXX') : (r.display_name || (r.student && r.student.name) || 'Anonymous');
    const count = r.counted_badges !== undefined ? r.counted_badges : 0;
    const isComplete = r.status === 'complete' || count >= goal;
    const lastDate = r.last_badge_date || '—';
    const badges = r.badges || [];

    const row = document.createElement('tr');
    row.className = 'roster-row';
    row.setAttribute('tabindex', '0');
    row.setAttribute('role', 'button');
    row.setAttribute('aria-expanded', 'false');

    // Column 1: Learner Name
    const tdName = document.createElement('td');
    tdName.className = 'col-learner';
    const nameSpan = document.createElement('div');
    nameSpan.className = 'learner-name';
    nameSpan.textContent = name;
    tdName.appendChild(nameSpan);
    row.appendChild(tdName);

    // Column 2: 10-Segment Progress Bar
    const tdProg = document.createElement('td');
    tdProg.setAttribute('data-label', 'Progress');
    tdProg.appendChild(render10SegmentBar(count, goal, isComplete));
    row.appendChild(tdProg);

    // Column 3: Count
    const tdCount = document.createElement('td');
    tdCount.className = 'count-cell';
    tdCount.setAttribute('data-label', 'Count');
    tdCount.textContent = `${count}/${goal}`;
    row.appendChild(tdCount);

    // Column 4: Status
    const tdStatus = document.createElement('td');
    tdStatus.setAttribute('data-label', 'Status');
    tdStatus.appendChild(renderStatusPill(r.status));
    row.appendChild(tdStatus);

    // Column 5: Last Earned Date
    const tdDate = document.createElement('td');
    tdDate.className = 'date-cell';
    tdDate.setAttribute('data-label', 'Last Earned');
    tdDate.textContent = lastDate;
    row.appendChild(tdDate);

    tbody.appendChild(row);

    // Expandable details row
    if (showBadges && badges.length > 0) {
      const detailRow = document.createElement('tr');
      detailRow.className = 'detail-row';

      const detailTd = document.createElement('td');
      detailTd.setAttribute('colspan', '5');
      detailTd.className = 'detail-content';

      const badgeList = document.createElement('ul');
      badgeList.className = 'badge-list';

      for (const b of badges) {
        const item = document.createElement('li');
        item.className = 'badge-item';

        const bTitle = document.createElement('div');
        bTitle.className = 'badge-title';
        bTitle.textContent = b.title;

        const bDate = document.createElement('div');
        bDate.className = 'badge-date';
        bDate.textContent = `Earned: ${b.earned_at || '—'}`;

        item.appendChild(bTitle);
        item.appendChild(bDate);
        badgeList.appendChild(item);
      }

      detailTd.appendChild(badgeList);
      detailRow.appendChild(detailTd);
      tbody.appendChild(detailRow);

      // Toggle row expansion on click / Enter
      const toggle = () => {
        const isOpen = detailRow.classList.toggle('open');
        row.setAttribute('aria-expanded', String(isOpen));
      };
      row.addEventListener('click', toggle);
      row.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          toggle();
        }
      });
    }
  }

  table.appendChild(tbody);
  container.appendChild(table);
  return container;
}

/**
 * Builds the "Couldn't be Checked" ledger section with required explanatory text and retry button.
 */
export function renderUnreachableSection(unreachableList, onRetry) {
  if (!unreachableList || unreachableList.length === 0) {
    return document.createDocumentFragment();
  }

  const section = document.createElement('section');
  section.className = 'unreachable-section';
  section.setAttribute('aria-label', "Profiles that couldn't be checked");

  const header = document.createElement('div');
  header.className = 'unreachable-header';

  const title = document.createElement('h3');
  title.className = 'unreachable-title';
  title.textContent = `Couldn't be Checked (${unreachableList.length})`;
  header.appendChild(title);

  // Check if any are retryable (network error, rate limited, parse error — never private)
  const hasRetryable = unreachableList.some(u => u.retryable || u.status === 'network_error' || u.status === 'rate_limited');
  if (hasRetryable && typeof onRetry === 'function') {
    const retryBtn = document.createElement('button');
    retryBtn.type = 'button';
    retryBtn.className = 'btn-secondary';
    retryBtn.textContent = 'Retry Unreachable';
    retryBtn.addEventListener('click', onRetry);
    header.appendChild(retryBtn);
  }

  section.appendChild(header);

  // Mandatory required sentence
  const notice = document.createElement('p');
  notice.className = 'unreachable-notice';
  notice.textContent = 'These profiles could not be read. They are not counted as zero.';
  section.appendChild(notice);

  // Table of unreachable profiles
  const table = document.createElement('table');
  table.className = 'roster-table';

  const thead = document.createElement('thead');
  const trHead = document.createElement('tr');
  const th1 = document.createElement('th');
  th1.textContent = 'Learner';
  const th2 = document.createElement('th');
  th2.textContent = 'Status Reason';
  trHead.appendChild(th1);
  trHead.appendChild(th2);
  thead.appendChild(trHead);
  table.appendChild(thead);

  const tbody = document.createElement('tbody');
  for (const u of unreachableList) {
    const tr = document.createElement('tr');

    const td1 = document.createElement('td');
    td1.className = 'col-learner';
    const nameEl = document.createElement('div');
    nameEl.className = 'learner-name';
    nameEl.textContent = u.pseudonym || u.display_name || 'Participant';
    td1.appendChild(nameEl);

    const td2 = document.createElement('td');
    td2.className = 'col-reason';
    const pill = renderStatusPill(u.status);
    const reasonText = document.createElement('span');
    reasonText.style.marginLeft = '8px';
    reasonText.style.color = 'var(--ink-secondary)';
    reasonText.style.fontSize = '12px';
    reasonText.textContent = u.reason || '';

    td2.appendChild(pill);
    td2.appendChild(reasonText);

    tr.appendChild(td1);
    tr.appendChild(td2);
    tbody.appendChild(tr);
  }

  table.appendChild(tbody);
  section.appendChild(table);
  return section;
}

/**
 * Builds the Run Report metrics strip at the bottom.
 */
export function renderReportStrip(summary = {}) {
  const strip = document.createElement('div');
  strip.className = 'report-strip';

  const metrics = [
    { label: 'Profiles Checked', value: summary.total_students || 0 },
    { label: 'Network Requests', value: summary.network_requests || 0 },
    { label: 'Cache Hits', value: summary.cache_hits || 0 },
    { label: 'Retries', value: summary.retries || 0 },
    { label: 'Rate-Limit Pause', value: `${summary.rate_limit_pause_seconds ? summary.rate_limit_pause_seconds.toFixed(1) : '0.0'}s` },
    { label: 'Duration', value: `${summary.duration_seconds ? summary.duration_seconds.toFixed(1) : '0.0'}s` },
    { label: 'Bad CSV Rows', value: summary.bad_rows_count || 0 }
  ];

  for (const m of metrics) {
    const item = document.createElement('div');
    item.className = 'report-metric';

    const lbl = document.createElement('span');
    lbl.className = 'report-metric-lbl';
    lbl.textContent = m.label;

    const val = document.createElement('span');
    val.className = 'report-metric-val';
    val.textContent = String(m.value);

    item.appendChild(lbl);
    item.appendChild(val);
    strip.appendChild(item);
  }

  return strip;
}

/**
 * Builds the Live Progress Strip during active streaming.
 */
export function renderProgressStrip(stats, onCancel) {
  const container = document.createElement('div');
  container.className = 'progress-strip';

  const header = document.createElement('div');
  header.className = 'strip-header';

  const statusEl = document.createElement('div');
  statusEl.className = 'strip-status';
  statusEl.textContent = stats.isPaused ? `Rate limit cool-down (~${stats.pauseEta || 5}s)` : 'Evaluating Roster...';
  header.appendChild(statusEl);

  if (typeof onCancel === 'function') {
    const cancelBtn = document.createElement('button');
    cancelBtn.type = 'button';
    cancelBtn.className = 'btn-danger';
    cancelBtn.textContent = 'Cancel Run';
    cancelBtn.addEventListener('click', onCancel);
    header.appendChild(cancelBtn);
  }
  container.appendChild(header);

  // Overall percentage bar
  const pct = stats.total > 0 ? Math.round((stats.done / stats.total) * 100) : 0;
  const bar = document.createElement('div');
  bar.className = 'overall-progress-bar';

  const fill = document.createElement('div');
  fill.className = 'overall-progress-fill';
  fill.style.width = `${pct}%`;
  bar.appendChild(fill);
  container.appendChild(bar);

  // Metrics details
  const metrics = document.createElement('div');
  metrics.className = 'strip-metrics';

  const countSpan = document.createElement('span');
  countSpan.textContent = `Progress: ${stats.done || 0} / ${stats.total || 0} (${pct}%)`;

  const rateSpan = document.createElement('span');
  rateSpan.textContent = `Rate: ${stats.rate ? stats.rate.toFixed(1) : '1.0'} req/s`;

  const cacheSpan = document.createElement('span');
  cacheSpan.textContent = `Cached: ${stats.cached || 0}`;

  const etaSpan = document.createElement('span');
  etaSpan.textContent = `ETA: ~${stats.eta || '0s'}`;

  metrics.appendChild(countSpan);
  metrics.appendChild(rateSpan);
  metrics.appendChild(cacheSpan);
  metrics.appendChild(etaSpan);
  container.appendChild(metrics);

  return container;
}

/**
 * Renders state messaging cards (empty, error, expired, zero results).
 */
export function renderStateMessage(title, text, isError = false) {
  const box = document.createElement('div');
  box.className = 'state-message-box';
  if (isError) box.style.borderColor = 'var(--status-error)';

  const h3 = document.createElement('h3');
  h3.className = 'state-message-title';
  h3.textContent = title;
  box.appendChild(h3);

  const p = document.createElement('p');
  p.className = 'state-message-text';
  p.textContent = text;
  box.appendChild(p);

  return box;
}

/**
 * Achievement Tiers for BADGE ARENA.
 */
export function getTier(count) {
  if (count >= 10) return { name: 'Champion', icon: '🏆', cls: 'tier-champion' };
  if (count >= 8)  return { name: 'Elite', icon: '⚡', cls: 'tier-elite' };
  if (count >= 5)  return { name: 'Challenger', icon: '🔥', cls: 'tier-challenger' };
  if (count >= 3)  return { name: 'Explorer', icon: '🧭', cls: 'tier-explorer' };
  if (count >= 1)  return { name: 'Rookie', icon: '🌱', cls: 'tier-rookie' };
  return { name: 'Not Started', icon: '⏸', cls: 'tier-not-started' };
}

/**
 * Calculates next milestone distance.
 */
export function getNextMilestone(count) {
  if (count >= 10) return 'Champion 10/10 Reached!';
  if (count >= 8)  return `${10 - count} to Champion`;
  if (count >= 5)  return `${8 - count} to Elite`;
  if (count >= 3)  return `${5 - count} to Challenger`;
  if (count >= 1)  return `${3 - count} to Explorer`;
  return '1 to Rookie';
}

/**
 * Pure DOM builder for BADGE ARENA gamified view.
 */
export function renderArenaView(results, options = {}) {
  const container = document.createElement('div');
  container.className = 'arena-container';

  const goal = options.goal || 10;
  const anonymize = options.anonymize || false;
  const prevRankMap = options.prevRankMap || {};

  // Sort candidates by badges desc, then name asc
  const valid = results.filter(r => r.Status !== 'unreachable' && r.Status !== 'private_or_not_found');
  const sorted = [...valid].sort((a, b) => {
    const diff = (b.CountedBadges || 0) - (a.CountedBadges || 0);
    if (diff !== 0) return diff;
    return (a.DisplayName || '').localeCompare(b.DisplayName || '');
  });

  // Assign standard competition ranks (1, 1, 3 for ties)
  let currentRank = 1;
  const ranked = sorted.map((item, idx) => {
    if (idx > 0 && (item.CountedBadges || 0) < (sorted[idx - 1].CountedBadges || 0)) {
      currentRank = idx + 1;
    }
    const prev = prevRankMap[item.Student?.UUID || item.Student?.ProfileURL];
    let movement = '=';
    let moveClass = 'move-neutral';
    if (prev !== undefined && prev !== null) {
      if (prev > currentRank) {
        movement = `↑ +${prev - currentRank}`;
        moveClass = 'move-up';
      } else if (prev < currentRank) {
        movement = `↓ -${currentRank - prev}`;
        moveClass = 'move-down';
      }
    }
    return { ...item, rank: currentRank, movement, moveClass };
  });

  // 1. Campaign Overview Stats Cards
  const champions = ranked.filter(r => (r.CountedBadges || 0) >= goal).length;
  const totalBadges = ranked.reduce((acc, r) => acc + (r.CountedBadges || 0), 0);
  const completionRate = ranked.length > 0 ? Math.round((champions / ranked.length) * 100) : 0;

  const statsRow = document.createElement('div');
  statsRow.className = 'arena-stats-grid';

  const cards = [
    { label: 'Active Learners', val: String(ranked.length), sub: 'Total In Roster', cls: 'stat-total' },
    { label: 'Champions (10/10)', val: String(champions), sub: `${completionRate}% Goal Met`, cls: 'stat-champions' },
    { label: 'Badges Earned', val: String(totalBadges), sub: `Avg ${(ranked.length ? (totalBadges / ranked.length).toFixed(1) : 0)} / learner`, cls: 'stat-badges' },
    { label: 'Arena Leader', val: ranked.length > 0 ? (anonymize ? (ranked[0].Pseudonym || 'Learner-1') : ranked[0].DisplayName) : '—', sub: ranked.length > 0 ? `${ranked[0].CountedBadges || 0} Badges` : 'Awaiting Data', cls: 'stat-leader' }
  ];

  for (const c of cards) {
    const cardEl = document.createElement('div');
    cardEl.className = `arena-stat-card ${c.cls}`;
    const l = document.createElement('div');
    l.className = 'arena-stat-label';
    l.textContent = c.label;
    const v = document.createElement('div');
    v.className = 'arena-stat-val';
    v.textContent = c.val;
    const s = document.createElement('div');
    s.className = 'arena-stat-sub';
    s.textContent = c.sub;
    cardEl.appendChild(l);
    cardEl.appendChild(v);
    cardEl.appendChild(s);
    statsRow.appendChild(cardEl);
  }
  container.appendChild(statsRow);

  // 2. Champions Celebration Banner (if champions exist)
  if (champions > 0) {
    const banner = document.createElement('div');
    banner.className = 'arena-celebration-banner';
    const bTitle = document.createElement('div');
    bTitle.className = 'celebration-title';
    bTitle.textContent = `🎉 HALL OF CHAMPIONS: ${champions} LEARNER${champions > 1 ? 'S' : ''} REACHED THE 10/10 GOAL! 🎉`;
    banner.appendChild(bTitle);
    container.appendChild(banner);
  }

  // 3. Champions Podium (Top 3)
  if (ranked.length > 0) {
    const podiumBox = document.createElement('div');
    podiumBox.className = 'arena-podium-section';
    const pTitle = document.createElement('h3');
    pTitle.className = 'arena-section-title';
    pTitle.textContent = 'CHAMPIONS PODIUM';
    podiumBox.appendChild(pTitle);

    const podium = document.createElement('div');
    podium.className = 'arena-podium';

    // Podium spots: 2nd (Silver, left), 1st (Gold, center), 3rd (Bronze, right)
    const spots = [];
    if (ranked.length > 1) spots.push({ item: ranked[1], place: '2nd', badgeCls: 'podium-silver', icon: '🥈', rankNum: '#2' });
    spots.push({ item: ranked[0], place: '1st', badgeCls: 'podium-gold', icon: '🏆', rankNum: '#1' });
    if (ranked.length > 2) spots.push({ item: ranked[2], place: '3rd', badgeCls: 'podium-bronze', icon: '🥉', rankNum: '#3' });

    for (const spot of spots) {
      const stand = document.createElement('div');
      stand.className = `podium-stand ${spot.badgeCls}`;

      const medal = document.createElement('div');
      medal.className = 'podium-icon';
      medal.textContent = spot.icon;

      const placeLabel = document.createElement('div');
      placeLabel.className = 'podium-place';
      placeLabel.textContent = spot.rankNum;

      const name = document.createElement('div');
      name.className = 'podium-name';
      const displayName = anonymize ? (spot.item.Pseudonym || spot.item.DisplayName) : spot.item.DisplayName;
      name.textContent = displayName;

      const badgeCount = document.createElement('div');
      badgeCount.className = 'podium-badges';
      badgeCount.textContent = `${spot.item.CountedBadges || 0} / 10 Badges`;

      const tier = getTier(spot.item.CountedBadges || 0);
      const tierBadge = document.createElement('span');
      tierBadge.className = `tier-pill ${tier.cls}`;
      tierBadge.textContent = `${tier.icon} ${tier.name}`;

      stand.appendChild(medal);
      stand.appendChild(placeLabel);
      stand.appendChild(name);
      stand.appendChild(badgeCount);
      stand.appendChild(tierBadge);
      podium.appendChild(stand);
    }

    podiumBox.appendChild(podium);
    container.appendChild(podiumBox);
  }

  // 4. Competitive Leaderboard Table
  const boardSection = document.createElement('div');
  boardSection.className = 'arena-leaderboard-section';

  const boardTitle = document.createElement('h3');
  boardTitle.className = 'arena-section-title';
  boardTitle.textContent = 'COMPETITIVE STANDINGS';
  boardSection.appendChild(boardTitle);

  const table = document.createElement('div');
  table.className = 'arena-table';

  // Header Row
  const header = document.createElement('div');
  header.className = 'arena-header-row';
  const cols = ['Rank', 'Learner', 'Achievement Tier', 'Progress (10 Badges)', 'Next Milestone', 'Trend'];
  for (const c of cols) {
    const th = document.createElement('div');
    th.className = 'arena-th';
    th.textContent = c;
    header.appendChild(th);
  }
  table.appendChild(header);

  // Rows
  for (const item of ranked) {
    const row = document.createElement('div');
    row.className = `arena-row rank-${item.rank <= 3 ? item.rank : 'standard'}`;

    // 1. Rank Col
    const colRank = document.createElement('div');
    colRank.className = 'arena-cell cell-rank';
    const rankBadge = document.createElement('span');
    rankBadge.className = `arena-rank-badge rank-${item.rank <= 3 ? item.rank : 'default'}`;
    rankBadge.textContent = `#${item.rank}`;
    colRank.appendChild(rankBadge);

    // 2. Learner Col
    const colName = document.createElement('div');
    colName.className = 'arena-cell cell-name';
    const nameEl = document.createElement('span');
    nameEl.className = 'arena-learner-name';
    nameEl.textContent = anonymize ? (item.Pseudonym || item.DisplayName) : item.DisplayName;
    colName.appendChild(nameEl);

    // 3. Tier Col
    const colTier = document.createElement('div');
    colTier.className = 'arena-cell cell-tier';
    const tier = getTier(item.CountedBadges || 0);
    const tierPill = document.createElement('span');
    tierPill.className = `tier-pill ${tier.cls}`;
    tierPill.textContent = `${tier.icon} ${tier.name}`;
    colTier.appendChild(tierPill);

    // 4. Progress Col
    const colProg = document.createElement('div');
    colProg.className = 'arena-cell cell-prog';
    const segBar = render10SegmentBar(item.CountedBadges || 0, goal, (item.CountedBadges || 0) >= goal);
    const countLabel = document.createElement('span');
    countLabel.className = 'arena-count-label';
    countLabel.textContent = `${item.CountedBadges || 0}/${goal}`;
    colProg.appendChild(segBar);
    colProg.appendChild(countLabel);

    // 5. Next Milestone
    const colMilestone = document.createElement('div');
    colMilestone.className = 'arena-cell cell-milestone';
    const milestoneSpan = document.createElement('span');
    milestoneSpan.className = 'arena-milestone-tag';
    milestoneSpan.textContent = getNextMilestone(item.CountedBadges || 0);
    colMilestone.appendChild(milestoneSpan);

    // 6. Trend / Movement
    const colMove = document.createElement('div');
    colMove.className = `arena-cell cell-trend ${item.moveClass}`;
    colMove.textContent = item.movement;

    row.appendChild(colRank);
    row.appendChild(colName);
    row.appendChild(colTier);
    row.appendChild(colProg);
    row.appendChild(colMilestone);
    row.appendChild(colMove);
    table.appendChild(row);
  }

  boardSection.appendChild(table);
  container.appendChild(boardSection);

  return container;
}
