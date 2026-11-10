/**
 * Google Skills Badgeboard - Static Dashboard Application
 * Zero-dependency, pure vanilla JS. All dynamic rendering uses textContent (NEVER innerHTML).
 */

(function () {
  'use strict';

  // Application State
  const state = {
    data: null,
    results: [],
    filteredResults: [],
    goal: 10,
    searchQuery: '',
    selectedStatus: 'all',
    sortBy: 'badges-desc',
    expandedRows: new Set(),
    isPublicTier: false,
    theme: 'system',
  };

  // DOM Element References (Safe initialization for both browser and test environments)
  const isBrowser = typeof document !== 'undefined';
  const elements = isBrowser ? {
    themeToggle: document.getElementById('themeToggle'),
    themeIcon: document.getElementById('themeIcon'),
    themeLabel: document.getElementById('themeLabel'),

    kpiTotal: document.getElementById('kpiTotal'),
    kpiGoalSubtext: document.getElementById('kpiGoalSubtext'),
    kpiComplete: document.getElementById('kpiComplete'),
    kpiCompletePct: document.getElementById('kpiCompletePct'),
    kpiInProgress: document.getElementById('kpiInProgress'),
    kpiNoBadges: document.getElementById('kpiNoBadges'),
    kpiUnavailable: document.getElementById('kpiUnavailable'),

    searchInput: document.getElementById('searchInput'),
    searchClear: document.getElementById('searchClear'),
    statusFilterGroup: document.getElementById('statusFilterGroup'),
    sortSelect: document.getElementById('sortSelect'),
    btnExportCSV: document.getElementById('btnExportCSV'),

    resultsCount: document.getElementById('resultsCount'),
    tierBadge: document.getElementById('tierBadge'),
    resultsContainer: document.getElementById('resultsContainer'),
    lastUpdated: document.getElementById('lastUpdated'),
  } : {};

  // ==========================================================================
  // Initialization & Data Loading
  // ==========================================================================

  async function init() {
    initTheme();
    setupEventListeners();
    await loadData();
  }

  async function loadData() {
    setLoadingState();
    try {
      // Relative path for GitHub Pages
      const response = await fetch('data/results.json', { cache: 'no-cache' });
      if (!response.ok) {
        throw new Error('HTTP ' + response.status + ' ' + response.statusText);
      }
      const data = await response.json();
      state.data = data;
      state.results = Array.isArray(data.results) ? data.results : [];
      state.goal = (data.summary && data.summary.goal > 0) ? data.summary.goal : 10;
      
      // Check if dataset is in Public Anonymized tier (no badges attached to any student)
      state.isPublicTier = (data.summary && data.summary.anonymized) || 
        state.results.every(r => !r.badges || r.badges.length === 0);

      renderKPIs(data.summary);
      applyFilters();
    } catch (err) {
      renderErrorState('Failed to load campaign data: ' + err.message);
    }
  }

  // ==========================================================================
  // Theme Management
  // ==========================================================================

  function initTheme() {
    const urlTheme = new URLSearchParams(window.location.search).get('theme');
    if (urlTheme === 'light' || urlTheme === 'dark') {
      applyTheme(urlTheme);
      return;
    }
    const savedTheme = localStorage.getItem('badgeboard-theme');
    if (savedTheme === 'light' || savedTheme === 'dark') {
      applyTheme(savedTheme);
    } else {
      const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
      applyTheme(prefersDark ? 'dark' : 'light');
    }

    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', (e) => {
      if (!localStorage.getItem('badgeboard-theme') && !new URLSearchParams(window.location.search).get('theme')) {
        applyTheme(e.matches ? 'dark' : 'light');
      }
    });
  }

  function applyTheme(theme) {
    state.theme = theme;
    document.documentElement.setAttribute('data-theme', theme);
    if (!elements.themeIcon) return;

    while (elements.themeIcon.firstChild) {
      elements.themeIcon.removeChild(elements.themeIcon.firstChild);
    }

    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('width', '16');
    svg.setAttribute('height', '16');
    svg.setAttribute('viewBox', '0 0 24 24');
    svg.setAttribute('fill', 'none');
    svg.setAttribute('stroke', 'currentColor');
    svg.setAttribute('stroke-width', '2');
    svg.setAttribute('stroke-linecap', 'round');
    svg.setAttribute('stroke-linejoin', 'round');

    if (theme === 'dark') {
      // Sun icon for switching to light mode
      const circle = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
      circle.setAttribute('cx', '12');
      circle.setAttribute('cy', '12');
      circle.setAttribute('r', '5');
      svg.appendChild(circle);

      const rays = [
        'M12 1v2', 'M12 21v2', 'M4.22 4.22l1.42 1.42', 'M18.36 18.36l1.42 1.42',
        'M1 12h2', 'M21 12h2', 'M4.22 19.78l1.42-1.42', 'M18.36 5.64l1.42-1.42'
      ];
      rays.forEach(d => {
        const line = document.createElementNS('http://www.w3.org/2000/svg', 'path');
        line.setAttribute('d', d);
        svg.appendChild(line);
      });

      elements.themeLabel.textContent = 'Light';
      elements.themeToggle.setAttribute('aria-label', 'Switch to light theme');
    } else {
      // Moon icon for switching to dark mode
      const moon = document.createElementNS('http://www.w3.org/2000/svg', 'path');
      moon.setAttribute('d', 'M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z');
      svg.appendChild(moon);

      elements.themeLabel.textContent = 'Dark';
      elements.themeToggle.setAttribute('aria-label', 'Switch to dark theme');
    }

    elements.themeIcon.appendChild(svg);
  }

  function toggleTheme() {
    const newTheme = state.theme === 'dark' ? 'light' : 'dark';
    localStorage.setItem('badgeboard-theme', newTheme);
    applyTheme(newTheme);
  }

  // ==========================================================================
  // Event Listeners
  // ==========================================================================

  function setupEventListeners() {
    if (elements.themeToggle) {
      elements.themeToggle.addEventListener('click', toggleTheme);
    }

    if (elements.searchInput) {
      elements.searchInput.addEventListener('input', (e) => {
        state.searchQuery = e.target.value.trim().toLowerCase();
        if (state.searchQuery.length > 0) {
          elements.searchClear.classList.remove('hidden');
        } else {
          elements.searchClear.classList.add('hidden');
        }
        applyFilters();
      });
    }

    if (elements.searchClear) {
      elements.searchClear.addEventListener('click', () => {
        elements.searchInput.value = '';
        state.searchQuery = '';
        elements.searchClear.classList.add('hidden');
        elements.searchInput.focus();
        applyFilters();
      });
    }

    if (elements.statusFilterGroup) {
      const pills = elements.statusFilterGroup.querySelectorAll('.filter-pill');
      pills.forEach(pill => {
        const selectPill = () => {
          pills.forEach(btn => {
            btn.classList.remove('active');
            btn.setAttribute('aria-checked', 'false');
          });
          pill.classList.add('active');
          pill.setAttribute('aria-checked', 'true');
          state.selectedStatus = pill.getAttribute('data-status');
          applyFilters();
        };

        pill.addEventListener('click', selectPill);
        pill.addEventListener('keydown', (e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            selectPill();
          }
        });
      });
    }

    if (elements.sortSelect) {
      elements.sortSelect.addEventListener('change', (e) => {
        state.sortBy = e.target.value;
        applyFilters();
      });
    }

    if (elements.btnExportCSV) {
      elements.btnExportCSV.addEventListener('click', downloadCSV);
    }
  }

  // ==========================================================================
  // Filtering & Sorting
  // ==========================================================================

  function applyFilters() {
    let filtered = state.results.slice();

    // 1. Filter by Status
    if (state.selectedStatus !== 'all') {
      if (state.selectedStatus === 'unavailable') {
        filtered = filtered.filter(r => 
          r.status === 'private_or_not_found' ||
          r.status === 'network_error' ||
          r.status === 'rate_limited' ||
          r.status === 'parse_error' ||
          r.status === 'invalid_url'
        );
      } else {
        filtered = filtered.filter(r => r.status === state.selectedStatus);
      }
    }

    // 2. Filter by Search Query
    if (state.searchQuery) {
      filtered = filtered.filter(r => {
        const name = (r.student && r.student.name ? r.student.name : (r.display_name || '')).toLowerCase();
        const url = (r.url || (r.student && r.student.profile_url) || '').toLowerCase();
        const uuid = (r.uuid || (r.student && r.student.uuid) || '').toLowerCase();
        return name.includes(state.searchQuery) || url.includes(state.searchQuery) || uuid.includes(state.searchQuery);
      });
    }

    // 3. Sort Records
    filtered.sort((a, b) => {
      const nameA = (a.student && a.student.name ? a.student.name : (a.display_name || '')).toLowerCase();
      const nameB = (b.student && b.student.name ? b.student.name : (b.display_name || '')).toLowerCase();

      switch (state.sortBy) {
        case 'badges-desc':
          if (b.counted_badges !== a.counted_badges) {
            return b.counted_badges - a.counted_badges;
          }
          return nameA.localeCompare(nameB);

        case 'badges-asc':
          if (a.counted_badges !== b.counted_badges) {
            return a.counted_badges - b.counted_badges;
          }
          return nameA.localeCompare(nameB);

        case 'name-asc':
          return nameA.localeCompare(nameB);

        case 'date-desc': {
          const dateA = a.last_badge_date || (a.last_badge_earned_at ? a.last_badge_earned_at.slice(0, 10) : '') || '0000-00-00';
          const dateB = b.last_badge_date || (b.last_badge_earned_at ? b.last_badge_earned_at.slice(0, 10) : '') || '0000-00-00';
          if (dateB !== dateA) {
            return dateB.localeCompare(dateA);
          }
          return b.counted_badges - a.counted_badges;
        }

        default:
          return 0;
      }
    });

    state.filteredResults = filtered;
    renderResults();
  }

  // ==========================================================================
  // DOM Rendering (Pure textContent)
  // ==========================================================================

  function renderKPIs(summary) {
    if (!summary) return;

    elements.kpiTotal.textContent = String(summary.total_students || 0);
    elements.kpiGoalSubtext.textContent = 'Target: ' + state.goal + ' badges';

    const completeCount = summary.complete_count || 0;
    elements.kpiComplete.textContent = String(completeCount);
    const total = summary.total_students || 1;
    const pct = Math.round((completeCount / total) * 100);
    elements.kpiCompletePct.textContent = pct + '% completion rate';

    elements.kpiInProgress.textContent = String(summary.in_progress_count || 0);
    elements.kpiNoBadges.textContent = String(summary.no_badges_count || 0);

    const unavailableCount = (summary.private_or_not_found_count || 0) + (summary.error_count || 0);
    elements.kpiUnavailable.textContent = String(unavailableCount);

    if (summary.generated_at && elements.lastUpdated) {
      try {
        const d = new Date(summary.generated_at);
        elements.lastUpdated.textContent = 'Updated ' + d.toLocaleDateString(undefined, {
          month: 'short',
          day: 'numeric',
          year: 'numeric',
          hour: '2-digit',
          minute: '2-digit',
          timeZoneName: 'short',
        });
      } catch (_) {
        elements.lastUpdated.textContent = 'Updated recently';
      }
    }

    if (elements.tierBadge) {
      if (state.isPublicTier) {
        elements.tierBadge.textContent = 'Public View';
        elements.tierBadge.title = 'Participant pseudonyms with progress summary (badge details omitted)';
      } else {
        elements.tierBadge.textContent = 'Full View';
        elements.tierBadge.title = 'Detailed participant view with expandable badge lists';
      }
    }
  }

  function renderResults() {
    const container = elements.resultsContainer;
    if (!container) return;

    while (container.firstChild) {
      container.removeChild(container.firstChild);
    }

    const totalCount = state.results.length;
    const filteredCount = state.filteredResults.length;
    elements.resultsCount.textContent = 'Showing ' + filteredCount + ' of ' + totalCount + ' participants';

    if (filteredCount === 0) {
      const emptyDiv = document.createElement('div');
      emptyDiv.className = 'empty-state';

      const title = document.createElement('div');
      title.className = 'empty-state-title';
      title.textContent = 'No matching participants found';

      const desc = document.createElement('p');
      desc.textContent = 'Try adjusting your search query or selecting a different status filter.';

      emptyDiv.appendChild(title);
      emptyDiv.appendChild(desc);
      container.appendChild(emptyDiv);
      return;
    }

    state.filteredResults.forEach((record, index) => {
      const card = createStudentCard(record, index);
      container.appendChild(card);
    });
  }

  function createStudentCard(record, index) {
    const card = document.createElement('div');
    card.className = 'student-card';
    const rowId = 'student-row-' + index;
    card.id = rowId;

    const hasBadges = Array.isArray(record.badges) && record.badges.length > 0;
    const isExpanded = state.expandedRows.has(rowId);
    if (isExpanded && hasBadges) {
      card.classList.add('is-expanded');
    }

    const mainRow = document.createElement('div');
    mainRow.className = 'card-main-row';
    if (!hasBadges) {
      mainRow.classList.add('non-expandable');
    } else {
      mainRow.setAttribute('role', 'button');
      mainRow.setAttribute('tabindex', '0');
      mainRow.setAttribute('aria-expanded', isExpanded ? 'true' : 'false');
    }

    // 1. Student Identity Info
    const infoCol = document.createElement('div');
    infoCol.className = 'student-info';

    const nameSpan = document.createElement('span');
    nameSpan.className = 'student-name';
    const nameText = (record.student && record.student.name) ? record.student.name : (record.display_name || 'Participant');
    nameSpan.textContent = nameText;

    const subinfoSpan = document.createElement('span');
    subinfoSpan.className = 'student-subinfo';
    if (record.league) {
      subinfoSpan.textContent = record.league + (record.points > 0 ? ' • ' + record.points + ' pts' : '');
    } else if (record.error) {
      subinfoSpan.textContent = record.error;
    } else {
      subinfoSpan.textContent = 'Google Skills Participant';
    }

    infoCol.appendChild(nameSpan);
    infoCol.appendChild(subinfoSpan);

    // 2. Progress Bar
    const progressCol = document.createElement('div');
    progressCol.className = 'student-progress';

    const track = document.createElement('div');
    track.className = 'progress-bar-bg';

    const fill = document.createElement('div');
    fill.className = 'progress-bar-fill';
    const counted = record.counted_badges || 0;
    const pct = Math.min(Math.round((counted / state.goal) * 100), 100);
    fill.style.width = pct + '%';
    if (counted >= state.goal) {
      fill.classList.add('complete');
    }
    track.appendChild(fill);

    const labelsRow = document.createElement('div');
    labelsRow.className = 'progress-labels';
    const countText = document.createElement('span');
    countText.textContent = counted + ' / ' + state.goal + ' badges';
    const pctText = document.createElement('span');
    pctText.textContent = pct + '%';
    labelsRow.appendChild(countText);
    labelsRow.appendChild(pctText);

    progressCol.appendChild(track);
    progressCol.appendChild(labelsRow);

    // 3. Status Pill
    const statusCol = document.createElement('div');
    statusCol.className = 'student-status';

    const statusPill = document.createElement('span');
    statusPill.className = 'status-pill status-' + (record.status || 'unavailable');
    statusPill.textContent = getStatusLabel(record.status);
    statusCol.appendChild(statusPill);

    // 4. Last Activity Date
    const dateCol = document.createElement('div');
    dateCol.className = 'date-cell';
    const rawDate = record.last_badge_date || (record.last_badge_earned_at ? record.last_badge_earned_at.slice(0, 10) : '');
    dateCol.textContent = formatDate(rawDate);

    // 5. Chevron Indicator
    const chevronCol = document.createElement('div');
    chevronCol.className = 'chevron-cell';

    if (hasBadges) {
      const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
      svg.setAttribute('class', 'chevron-icon');
      svg.setAttribute('viewBox', '0 0 24 24');
      svg.setAttribute('fill', 'none');
      svg.setAttribute('stroke', 'currentColor');
      svg.setAttribute('stroke-width', '2');
      svg.setAttribute('stroke-linecap', 'round');
      svg.setAttribute('stroke-linejoin', 'round');
      svg.setAttribute('aria-hidden', 'true');

      const polyline = document.createElementNS('http://www.w3.org/2000/svg', 'polyline');
      polyline.setAttribute('points', '6 9 12 15 18 9');
      svg.appendChild(polyline);
      chevronCol.appendChild(svg);
    }

    const toggleExpand = () => {
      if (!hasBadges) return;
      if (state.expandedRows.has(rowId)) {
        state.expandedRows.delete(rowId);
        card.classList.remove('is-expanded');
        mainRow.setAttribute('aria-expanded', 'false');
      } else {
        state.expandedRows.add(rowId);
        card.classList.add('is-expanded');
        mainRow.setAttribute('aria-expanded', 'true');
      }
    };

    if (hasBadges) {
      mainRow.addEventListener('click', toggleExpand);
      mainRow.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          toggleExpand();
        }
      });
    }

    mainRow.appendChild(infoCol);
    mainRow.appendChild(progressCol);
    mainRow.appendChild(statusCol);
    mainRow.appendChild(dateCol);
    mainRow.appendChild(chevronCol);
    card.appendChild(mainRow);

    // Accordion Drawer (for records with badge lists)
    if (hasBadges) {
      const drawer = document.createElement('div');
      drawer.className = 'badge-drawer';

      const drawerHeader = document.createElement('div');
      drawerHeader.className = 'drawer-header';
      const headerTitle = document.createElement('span');
      headerTitle.textContent = 'Earned Badges (' + record.badges.length + ')';
      drawerHeader.appendChild(headerTitle);
      drawer.appendChild(drawerHeader);

      const badgeList = document.createElement('div');
      badgeList.className = 'badge-list';

      record.badges.forEach((b, bIdx) => {
        const item = document.createElement('div');
        item.className = 'badge-item';

        const titleSpan = document.createElement('span');
        titleSpan.className = 'badge-item-title';
        titleSpan.textContent = (bIdx + 1) + '. ' + b.title;
        titleSpan.title = b.title;

        const bDateSpan = document.createElement('span');
        bDateSpan.className = 'badge-item-date';
        const bDate = b.earned_at ? b.earned_at.slice(0, 10) : (b.raw_earned || '');
        bDateSpan.textContent = formatDate(bDate);

        item.appendChild(titleSpan);
        item.appendChild(bDateSpan);
        badgeList.appendChild(item);
      });

      drawer.appendChild(badgeList);
      card.appendChild(drawer);
    } else if (state.isPublicTier && counted > 0) {
      // Public tier notice for completed or in-progress learners without individual badge lists
      const drawer = document.createElement('div');
      drawer.className = 'badge-drawer';
      const notice = document.createElement('div');
      notice.className = 'public-tier-notice';
      notice.textContent = 'Badge details are not published in the public view.';
      drawer.appendChild(notice);
      card.appendChild(drawer);
    }

    return card;
  }

  // ==========================================================================
  // Client-Side CSV Export with Formula Injection Neutralization
  // ==========================================================================

  /**
   * Neutralize spreadsheet formula injection for CSV export.
   * Prepends a single quote if string begins with =, +, -, @, \t, or \r.
   * Matches Go sanitizeCSVField behavior.
   */
  function sanitizeCSVCell(str) {
    if (str === null || str === undefined) return '';
    const s = String(str);
    if (s.length === 0) return '';
    const firstChar = s[0];
    let sanitized = s;
    if (firstChar === '=' || firstChar === '+' || firstChar === '-' || firstChar === '@' || firstChar === '\t' || firstChar === '\r') {
      sanitized = "'" + s;
    }
    if (sanitized.includes('"') || sanitized.includes(',') || sanitized.includes('\n') || sanitized.includes('\r')) {
      return '"' + sanitized.replace(/"/g, '""') + '"';
    }
    return sanitized;
  }

  function generateCSV(records, goal) {
    const isPublic = state.isPublicTier;
    const header = isPublic
      ? ['Name', 'Status', 'Counted Badges', 'Total Badges', 'Goal', 'Last Badge Date', 'Error']
      : ['Name', 'Profile URL', 'UUID', 'Status', 'Counted Badges', 'Total Badges', 'Goal', 'Last Badge Date', 'League', 'Points', 'Error'];

    const rows = [header.map(sanitizeCSVCell).join(',')];

    (records || []).forEach(r => {
      const name = (r.student && r.student.name) || r.display_name || 'Participant';
      const status = r.status || '';
      const counted = String(r.counted_badges || 0);
      const total = String(r.total_badges || 0);
      const targetGoal = String(goal || 10);
      const lastDate = r.last_badge_date || (r.last_badge_earned_at ? r.last_badge_earned_at.slice(0, 10) : '');
      const err = r.error || '';

      let rowData;
      if (isPublic) {
        rowData = [name, status, counted, total, targetGoal, lastDate, err];
      } else {
        const url = (r.student && r.student.profile_url) || '';
        const uuid = (r.student && r.student.uuid) || '';
        const league = r.league || '';
        const points = String(r.points || 0);
        rowData = [name, url, uuid, status, counted, total, targetGoal, lastDate, league, points, err];
      }
      rows.push(rowData.map(sanitizeCSVCell).join(','));
    });

    return rows.join('\r\n');
  }

  function downloadCSV() {
    const csvContent = generateCSV(state.filteredResults, state.goal);
    const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.setAttribute('href', url);
    link.setAttribute('download', 'badgeboard_export.csv');
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
  }

  // ==========================================================================
  // Helper Formatters
  // ==========================================================================

  function getStatusLabel(status) {
    switch (status) {
      case 'complete': return '✓ Complete';
      case 'in_progress': return 'In Progress';
      case 'no_badges': return 'No Badges';
      case 'private_or_not_found': return 'Private / 404';
      case 'network_error': return 'Network Err';
      case 'rate_limited': return 'Rate Limited';
      case 'parse_error': return 'Parse Error';
      default: return 'Unavailable';
    }
  }

  function formatDate(dateStr) {
    if (!dateStr || dateStr === '0001-01-01') return '—';
    try {
      const parts = dateStr.slice(0, 10).split('-');
      if (parts.length === 3) {
        const year = parseInt(parts[0], 10);
        const month = parseInt(parts[1], 10) - 1;
        const day = parseInt(parts[2], 10);
        const d = new Date(Date.UTC(year, month, day));
        return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric', timeZone: 'UTC' });
      }
      return dateStr;
    } catch (_) {
      return dateStr;
    }
  }

  function setLoadingState() {
    const container = elements.resultsContainer;
    if (!container) return;
    while (container.firstChild) {
      container.removeChild(container.firstChild);
    }
    const emptyDiv = document.createElement('div');
    emptyDiv.className = 'empty-state';
    const text = document.createElement('p');
    text.textContent = 'Loading participants from results.json...';
    emptyDiv.appendChild(text);
    container.appendChild(emptyDiv);
  }

  function renderErrorState(message) {
    const container = elements.resultsContainer;
    if (!container) return;
    while (container.firstChild) {
      container.removeChild(container.firstChild);
    }
    const errorDiv = document.createElement('div');
    errorDiv.className = 'empty-state';

    const title = document.createElement('div');
    title.className = 'empty-state-title';
    title.textContent = 'Unable to Load Campaign Data';

    const msg = document.createElement('p');
    msg.textContent = message;

    const retryBtn = document.createElement('button');
    retryBtn.type = 'button';
    retryBtn.className = 'btn-theme-toggle';
    retryBtn.style.marginTop = '14px';
    retryBtn.textContent = 'Retry';
    retryBtn.addEventListener('click', () => loadData());

    errorDiv.appendChild(title);
    errorDiv.appendChild(msg);
    errorDiv.appendChild(retryBtn);
    container.appendChild(errorDiv);
  }

  // Export for test suite (Node.js)
  if (typeof module !== 'undefined' && module.exports) {
    module.exports = { sanitizeCSVCell, generateCSV };
  }

  // Run on page load if browser
  if (isBrowser) {
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', init);
    } else {
      init();
    }
  }
})();
