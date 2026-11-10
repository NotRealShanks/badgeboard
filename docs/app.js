/**
 * Google Skills Badgeboard - Static Dashboard Application
 * Zero-dependency, pure vanilla JS. All rendering uses textContent (NEVER innerHTML).
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

  // DOM Element References
  const elements = {
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

    resultsCount: document.getElementById('resultsCount'),
    tierBadge: document.getElementById('tierBadge'),
    resultsContainer: document.getElementById('resultsContainer'),
    lastUpdated: document.getElementById('lastUpdated'),
  };

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
      // Primary relative path for GitHub Pages
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
      if (!localStorage.getItem('badgeboard-theme')) {
        applyTheme(e.matches ? 'dark' : 'light');
      }
    });
  }

  function applyTheme(theme) {
    state.theme = theme;
    document.documentElement.setAttribute('data-theme', theme);
    if (theme === 'dark') {
      elements.themeIcon.textContent = '☀️';
      elements.themeLabel.textContent = 'Light';
      elements.themeToggle.setAttribute('aria-label', 'Switch to light theme');
    } else {
      elements.themeIcon.textContent = '🌙';
      elements.themeLabel.textContent = 'Dark';
      elements.themeToggle.setAttribute('aria-label', 'Switch to dark theme');
    }
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
    elements.themeToggle.addEventListener('click', toggleTheme);

    elements.searchInput.addEventListener('input', (e) => {
      state.searchQuery = e.target.value.trim().toLowerCase();
      if (state.searchQuery.length > 0) {
        elements.searchClear.classList.remove('hidden');
      } else {
        elements.searchClear.classList.add('hidden');
      }
      applyFilters();
    });

    elements.searchClear.addEventListener('click', () => {
      elements.searchInput.value = '';
      state.searchQuery = '';
      elements.searchClear.classList.add('hidden');
      elements.searchInput.focus();
      applyFilters();
    });

    elements.statusFilterGroup.addEventListener('click', (e) => {
      const target = e.target.closest('.filter-pill');
      if (!target) return;

      const buttons = elements.statusFilterGroup.querySelectorAll('.filter-pill');
      buttons.forEach(btn => {
        btn.classList.remove('active');
        btn.setAttribute('aria-checked', 'false');
      });

      target.classList.add('active');
      target.setAttribute('aria-checked', 'true');
      state.selectedStatus = target.getAttribute('data-status');
      applyFilters();
    });

    elements.sortSelect.addEventListener('change', (e) => {
      state.sortBy = e.target.value;
      applyFilters();
    });
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

    // 2. Filter by Search Query (Name or Pseudonym)
    if (state.searchQuery) {
      filtered = filtered.filter(r => {
        const name = (r.student && r.student.name ? r.student.name : (r.display_name || '')).toLowerCase();
        return name.includes(state.searchQuery);
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
          const dateA = a.last_badge_date || (a.last_badge_earned_at ? a.last_badge_earned_at.slice(0, 10) : '');
          const dateB = b.last_badge_date || (b.last_badge_earned_at ? b.last_badge_earned_at.slice(0, 10) : '');
          if (dateA !== dateB) {
            return dateB.localeCompare(dateA);
          }
          return (b.counted_badges || 0) - (a.counted_badges || 0);
        }

        default:
          return 0;
      }
    });

    state.filteredResults = filtered;
    renderResults();
  }

  // ==========================================================================
  // Safe DOM Rendering (Strictly textContent / createTextNode)
  // ==========================================================================

  function renderKPIs(summary) {
    if (!summary) return;

    elements.kpiTotal.textContent = String(summary.total_students || 0);
    elements.kpiGoalSubtext.textContent = 'Campaign Goal: ' + state.goal + ' Badges';

    const completeCount = summary.complete_count || 0;
    elements.kpiComplete.textContent = String(completeCount);
    const total = summary.total_students || 1;
    const pct = Math.round((completeCount / total) * 100);
    elements.kpiCompletePct.textContent = pct + '% completion rate';

    elements.kpiInProgress.textContent = String(summary.in_progress_count || 0);
    elements.kpiNoBadges.textContent = String(summary.no_badges_count || 0);

    const unavailableCount = (summary.private_or_not_found_count || 0) + (summary.error_count || 0);
    elements.kpiUnavailable.textContent = String(unavailableCount);

    if (summary.generated_at) {
      try {
        const d = new Date(summary.generated_at);
        elements.lastUpdated.textContent = 'Updated: ' + d.toLocaleDateString(undefined, {
          month: 'short',
          day: 'numeric',
          year: 'numeric',
          hour: '2-digit',
          minute: '2-digit',
        });
      } catch (_) {
        elements.lastUpdated.textContent = 'Updated: Recently';
      }
    }

    if (state.isPublicTier) {
      elements.tierBadge.textContent = 'Public View';
      elements.tierBadge.title = 'Participant pseudonyms with progress summary (badge details omitted)';
    } else {
      elements.tierBadge.textContent = 'Full View';
      elements.tierBadge.title = 'Detailed participant view with expandable badge lists';
    }
  }

  function renderResults() {
    const container = elements.resultsContainer;
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
    progressCol.className = 'progress-container';

    const track = document.createElement('div');
    track.className = 'progress-track';

    const fill = document.createElement('div');
    fill.className = 'progress-bar-fill';
    const counted = record.counted_badges || 0;
    const pct = Math.min(Math.round((counted / state.goal) * 100), 100);
    fill.style.width = pct + '%';
    if (counted >= state.goal) {
      fill.classList.add('fill-complete');
    }
    track.appendChild(fill);

    const textRow = document.createElement('div');
    textRow.className = 'progress-text-row';
    const countText = document.createElement('span');
    countText.textContent = counted + ' / ' + state.goal + ' badges';
    const pctText = document.createElement('span');
    pctText.textContent = pct + '%';
    textRow.appendChild(countText);
    textRow.appendChild(pctText);

    progressCol.appendChild(track);
    progressCol.appendChild(textRow);

    // 3. Status Badge
    const statusCol = document.createElement('div');
    const badge = document.createElement('span');
    badge.className = 'status-badge ' + getStatusClass(record.status);
    badge.textContent = getStatusLabel(record.status);
    statusCol.appendChild(badge);

    // 4. Last Activity Date
    const dateCol = document.createElement('div');
    dateCol.className = 'date-cell';
    const rawDate = record.last_badge_date || (record.last_badge_earned_at ? record.last_badge_earned_at.slice(0, 10) : '');
    dateCol.textContent = formatDate(rawDate);

    // 5. Expand Button or Non-expandable Placeholder
    const actionCol = document.createElement('div');
    if (hasBadges) {
      const expandBtn = document.createElement('button');
      expandBtn.type = 'button';
      expandBtn.className = 'btn-expand';
      expandBtn.setAttribute('aria-expanded', isExpanded ? 'true' : 'false');
      expandBtn.setAttribute('aria-label', isExpanded ? 'Collapse badge details' : 'Expand badge details');

      const chevron = document.createElement('span');
      chevron.className = 'chevron-icon';
      chevron.textContent = '▼';
      expandBtn.appendChild(chevron);

      const toggleExpand = () => {
        if (state.expandedRows.has(rowId)) {
          state.expandedRows.delete(rowId);
          card.classList.remove('is-expanded');
          expandBtn.setAttribute('aria-expanded', 'false');
          expandBtn.setAttribute('aria-label', 'Expand badge details');
        } else {
          state.expandedRows.add(rowId);
          card.classList.add('is-expanded');
          expandBtn.setAttribute('aria-expanded', 'true');
          expandBtn.setAttribute('aria-label', 'Collapse badge details');
        }
      };

      expandBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        toggleExpand();
      });

      mainRow.addEventListener('click', toggleExpand);
      actionCol.appendChild(expandBtn);
    } else {
      const placeholder = document.createElement('span');
      placeholder.className = 'expand-placeholder';
      placeholder.textContent = '—';
      placeholder.title = 'Badge details are not published in the public view';
      actionCol.appendChild(placeholder);
    }

    mainRow.appendChild(infoCol);
    mainRow.appendChild(progressCol);
    mainRow.appendChild(statusCol);
    mainRow.appendChild(dateCol);
    mainRow.appendChild(actionCol);
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

  function getStatusClass(status) {
    switch (status) {
      case 'complete': return 'badge-complete';
      case 'in_progress': return 'badge-in_progress';
      case 'no_badges': return 'badge-no_badges';
      default: return 'badge-unavailable';
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

  // Run on page load
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
