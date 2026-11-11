// Google Skills Badgeboard — Frontend Application
import { DEMO_DATA, generateLargeDemoDataset } from './demo-data.js';
import { parseRosterCSV, generateCSVExport } from './csv.js';
import { generateOfflineReport, computeDistribution } from './report.js';
import {
  renderSummarySentence,
  renderDistributionBar,
  renderRosterTable,
  renderUnreachableSection,
  renderReportStrip,
  renderProgressStrip,
  renderStateMessage,
  renderArenaView
} from './render.js';

class BadgeboardApp {
  constructor() {
    this.mode = 'DEMO'; // 'LIVE' or 'DEMO'
    this.state = 'empty'; // 'empty', 'validating', 'starting', 'streaming', 'complete', 'partial', 'cancelled', 'error', 'expired', 'zero', 'huge'
    this.theme = 'light';
    this.activeView = 'arena'; // 'arena' (BADGE ARENA) or 'ledger' (Editorial Ledger)
    this.currentSnapshot = null;
    this.filteredResults = [];
    this.activeFilter = 'all';
    this.searchQuery = '';
    this.sortBy = 'progress-desc';
    this.activeTab = 'upload';
    this.streamingInterval = null;
    this.previousSnapshot = null;
    this.prevRankMap = {};
    this.sessionComparisonDiff = null;
    this.accessCode = null;
    this.pendingRunAction = null;

    this.settings = {
      goal: 10,
      rps: 1.0,
      concurrency: 2,
      anonymize: false,
      includeBadges: true,
      bypassCache: false
    };

    this.initDOM();
  }

  async init() {
    this.initTheme();

    // Dev preview switcher is strictly hidden unless ?dev=1 is explicitly passed
    const params = new URLSearchParams(window.location.search);
    if (params.get('dev') === '1' && this.elements.devBanner) {
      this.elements.devBanner.classList.add('dev-active');
    }

    await this.detectMode();
    this.bindEvents();

    // Check URL state param for dev/testing
    const devState = params.get('state');
    if (devState) {
      this.setState(devState);
    } else if (this.mode === 'DEMO') {
      // In standalone GitHub Pages, show complete demo state by default
      this.setState('complete');
    } else {
      this.setState('empty');
    }
  }

  initDOM() {
    this.elements = {
      headerTitle: document.getElementById('headerTitle'),
      btnNewRun: document.getElementById('btnNewRun'),
      themeBtn: document.getElementById('themeToggleBtn'),
      modeTag: document.getElementById('modeTag'),
      devBanner: document.querySelector('.dev-banner'),
      inputSection: document.getElementById('inputSection'),
      resultsSection: document.getElementById('resultsSection'),
      btnViewArena: document.getElementById('btnViewArena'),
      btnViewLedger: document.getElementById('btnViewLedger'),
      arenaViewContainer: document.getElementById('arenaViewContainer'),
      ledgerViewContainer: document.getElementById('ledgerViewContainer'),
      btnRefreshAll: document.getElementById('btnRefreshAll'),
      lastCheckedBadge: document.getElementById('lastCheckedBadge'),
      arenaPrivacyToggle: document.getElementById('arenaPrivacyToggle'),
      progressStripContainer: document.getElementById('progressStripContainer'),
      summaryContainer: document.getElementById('summaryContainer'),
      distContainer: document.getElementById('distContainer'),
      toolbarSection: document.getElementById('toolbarSection'),
      tableContainer: document.getElementById('tableContainer'),
      unreachableContainer: document.getElementById('unreachableContainer'),
      reportStripContainer: document.getElementById('reportStripContainer'),
      stateMessageContainer: document.getElementById('stateMessageContainer'),

      // Access Code Modal (Hosted Hardening)
      accessCodeModal: document.getElementById('accessCodeModal'),
      accessCodeInput: document.getElementById('accessCodeInput'),
      accessCodeError: document.getElementById('accessCodeError'),
      btnSubmitAccessCode: document.getElementById('btnSubmitAccessCode'),
      btnCancelAccessCode: document.getElementById('btnCancelAccessCode'),

      // Tabs
      tabUpload: document.getElementById('tabUpload'),
      tabPaste: document.getElementById('tabPaste'),
      tabSheet: document.getElementById('tabSheet'),
      paneUpload: document.getElementById('paneUpload'),
      panePaste: document.getElementById('panePaste'),
      paneSheet: document.getElementById('paneSheet'),

      // Inputs
      fileInput: document.getElementById('fileInput'),
      dropzone: document.getElementById('dropzone'),
      fileInfo: document.getElementById('fileInfo'),
      urlsTextarea: document.getElementById('urlsTextarea'),
      sheetInput: document.getElementById('sheetInput'),

      // Action buttons
      btnStart: document.getElementById('btnStart'),
      btnDemo: document.getElementById('btnDemo'),

      // Settings
      settingGoal: document.getElementById('settingGoal'),
      settingRps: document.getElementById('settingRps'),
      settingConcurrency: document.getElementById('settingConcurrency'),
      settingAnonymize: document.getElementById('settingAnonymize'),
      settingIncludeBadges: document.getElementById('settingIncludeBadges'),
      settingBypassCache: document.getElementById('settingBypassCache'),

      // Toolbar
      searchInput: document.getElementById('searchInput'),
      sortSelect: document.getElementById('sortSelect'),
      filterPills: document.querySelectorAll('.pill-btn'),
      btnExportCSV: document.getElementById('btnExportCSV'),
      btnExportJSON: document.getElementById('btnExportJSON'),
      btnExportReport: document.getElementById('btnExportReport'),
      btnPrint: document.getElementById('btnPrint')
    };
  }

  initTheme() {
    const params = new URLSearchParams(window.location.search);
    const urlTheme = params.get('theme');
    const saved = localStorage.getItem('badgeboard_theme');
    const prefersDark = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
    this.theme = urlTheme || saved || (prefersDark ? 'dark' : 'light');
    document.documentElement.setAttribute('data-theme', this.theme);
    this.updateThemeButton();
  }

  toggleTheme() {
    this.theme = this.theme === 'dark' ? 'light' : 'dark';
    document.documentElement.setAttribute('data-theme', this.theme);
    localStorage.setItem('badgeboard_theme', this.theme);
    this.updateThemeButton();
  }

  updateThemeButton() {
    if (!this.elements.themeBtn) return;
    this.elements.themeBtn.textContent = this.theme === 'dark' ? '☀ Light' : '☾ Dark';
  }

  async detectMode() {
    try {
      const res = await fetch('/healthz', { method: 'GET', cache: 'no-store' });
      if (res.ok) {
        this.mode = 'LIVE';
      } else {
        this.mode = 'DEMO';
      }
    } catch {
      this.mode = 'DEMO';
    }

    if (this.elements.modeTag) {
      this.elements.modeTag.textContent = this.mode === 'LIVE' ? 'Live Backend' : 'Demo Data';
      this.elements.modeTag.className = `mode-tag ${this.mode === 'LIVE' ? 'mode-live' : 'mode-demo'}`;
    }
  }

  bindEvents() {
    // Theme
    this.elements.themeBtn.addEventListener('click', () => this.toggleTheme());

    // Tabs
    this.elements.tabUpload.addEventListener('click', () => this.switchTab('upload'));
    this.elements.tabPaste.addEventListener('click', () => this.switchTab('paste'));
    this.elements.tabSheet.addEventListener('click', () => this.switchTab('sheet'));

    // Dropzone
    this.elements.dropzone.addEventListener('click', () => this.elements.fileInput.click());
    this.elements.dropzone.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        this.elements.fileInput.click();
      }
    });
    this.elements.fileInput.addEventListener('change', (e) => this.handleFileSelect(e.target.files[0]));

    this.elements.dropzone.addEventListener('dragover', (e) => {
      e.preventDefault();
      this.elements.dropzone.classList.add('drag-over');
    });
    this.elements.dropzone.addEventListener('dragleave', () => {
      this.elements.dropzone.classList.remove('drag-over');
    });
    this.elements.dropzone.addEventListener('drop', (e) => {
      e.preventDefault();
      this.elements.dropzone.classList.remove('drag-over');
      if (e.dataTransfer.files.length > 0) {
        this.handleFileSelect(e.dataTransfer.files[0]);
      }
    });

    // Access Code Modal Events (Hosted Hardening)
    if (this.elements.btnSubmitAccessCode) {
      this.elements.btnSubmitAccessCode.addEventListener('click', () => this.submitAccessCode());
    }
    if (this.elements.accessCodeInput) {
      this.elements.accessCodeInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
          e.preventDefault();
          this.submitAccessCode();
        }
      });
    }
    if (this.elements.btnCancelAccessCode) {
      this.elements.btnCancelAccessCode.addEventListener('click', () => {
        if (this.elements.accessCodeModal) {
          this.elements.accessCodeModal.classList.remove('is-open');
        }
        this.pendingRunAction = null;
        this.setState('empty');
      });
    }

    // View Switcher (BADGE ARENA vs Editorial Ledger)
    if (this.elements.btnViewArena) {
      this.elements.btnViewArena.addEventListener('click', () => this.switchView('arena'));
    }
    if (this.elements.btnViewLedger) {
      this.elements.btnViewLedger.addEventListener('click', () => this.switchView('ledger'));
    }
    if (this.elements.btnRefreshAll) {
      this.elements.btnRefreshAll.addEventListener('click', () => this.refreshAllLearners());
    }
    if (this.elements.arenaPrivacyToggle) {
      this.elements.arenaPrivacyToggle.addEventListener('change', (e) => {
        this.settings.anonymize = e.target.checked;
        if (this.elements.settingAnonymize) this.elements.settingAnonymize.checked = e.target.checked;
        this.renderResultsView();
      });
    }

    // Return to input / New Run navigation
    if (this.elements.btnNewRun) {
      this.elements.btnNewRun.addEventListener('click', () => {
        this.setState('empty');
      });
    }
    if (this.elements.headerTitle) {
      this.elements.headerTitle.addEventListener('click', () => {
        this.setState('empty');
      });
      this.elements.headerTitle.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          this.setState('empty');
        }
      });
    }

    // Action buttons
    this.elements.btnDemo.addEventListener('click', () => this.setState('complete'));
    this.elements.btnStart.addEventListener('click', () => this.startRun());

    // Search and Filters
    this.elements.searchInput.addEventListener('input', (e) => {
      this.searchQuery = e.target.value.trim().toLowerCase();
      this.applyFilterAndSort();
    });

    this.elements.sortSelect.addEventListener('change', (e) => {
      this.sortBy = e.target.value;
      this.applyFilterAndSort();
    });

    this.elements.filterPills.forEach(pill => {
      pill.addEventListener('click', () => {
        this.elements.filterPills.forEach(p => p.classList.remove('active'));
        pill.classList.add('active');
        this.activeFilter = pill.dataset.filter;
        this.applyFilterAndSort();
      });
    });

    // Export Buttons
    this.elements.btnExportCSV.addEventListener('click', () => this.exportCSV());
    this.elements.btnExportJSON.addEventListener('click', () => this.exportJSON());
    this.elements.btnExportReport.addEventListener('click', () => this.exportReportHTML());
    this.elements.btnPrint.addEventListener('click', () => window.print());

    // Dev state buttons
    document.querySelectorAll('.dev-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        e.preventDefault();
        const st = btn.dataset.state;
        this.setState(st);
      });
    });
  }

  switchTab(tab) {
    this.activeTab = tab;
    this.elements.tabUpload.classList.toggle('active', tab === 'upload');
    this.elements.tabPaste.classList.toggle('active', tab === 'paste');
    this.elements.tabSheet.classList.toggle('active', tab === 'sheet');

    this.elements.paneUpload.classList.toggle('active', tab === 'upload');
    this.elements.panePaste.classList.toggle('active', tab === 'paste');
    this.elements.paneSheet.classList.toggle('active', tab === 'sheet');
  }

  handleFileSelect(file) {
    if (!file) return;
    this.selectedFile = file;
    if (this.elements.fileInfo) {
      this.elements.fileInfo.classList.remove('hidden');
      this.elements.fileInfo.textContent = `Selected: ${file.name} (${Math.round(file.size / 1024)} KB)`;
    }
  }

  submitAccessCode() {
    if (!this.elements.accessCodeInput) return;
    const code = this.elements.accessCodeInput.value.trim();
    if (!code) {
      if (this.elements.accessCodeError) {
        this.elements.accessCodeError.classList.remove('hidden');
        this.elements.accessCodeError.textContent = 'Please enter an access code.';
      }
      return;
    }
    // Access code is kept in memory only — never stored in localStorage or sessionStorage
    this.accessCode = code;
    if (this.elements.accessCodeModal) {
      this.elements.accessCodeModal.classList.remove('is-open');
    }
    if (this.elements.accessCodeError) {
      this.elements.accessCodeError.classList.add('hidden');
    }
    if (this.pendingRunAction) {
      const action = this.pendingRunAction;
      this.pendingRunAction = null;
      action();
    }
  }

  switchView(view) {
    this.activeView = view;
    if (view === 'arena') {
      this.elements.btnViewArena?.classList.add('active');
      this.elements.btnViewArena?.setAttribute('aria-selected', 'true');
      this.elements.btnViewLedger?.classList.remove('active');
      this.elements.btnViewLedger?.setAttribute('aria-selected', 'false');
      this.elements.arenaViewContainer?.classList.remove('hidden');
      this.elements.ledgerViewContainer?.classList.add('hidden');
    } else {
      this.elements.btnViewLedger?.classList.add('active');
      this.elements.btnViewLedger?.setAttribute('aria-selected', 'true');
      this.elements.btnViewArena?.classList.remove('active');
      this.elements.btnViewArena?.setAttribute('aria-selected', 'false');
      this.elements.ledgerViewContainer?.classList.remove('hidden');
      this.elements.arenaViewContainer?.classList.add('hidden');
    }
  }

  refreshAllLearners() {
    if (this.elements.lastCheckedBadge) {
      this.elements.lastCheckedBadge.textContent = 'Refreshing...';
    }
    if (this.mode === 'DEMO') {
      this.startSimulatedStreaming();
    } else if (this.currentRunId) {
      fetch(`/api/runs/${this.currentRunId}/retry`, { method: 'POST' }).catch(() => {});
      this.subscribeLiveEvents(this.currentRunId);
    }
  }

  readSettings() {
    this.settings.goal = parseInt(this.elements.settingGoal.value, 10) || 10;
    this.settings.rps = parseFloat(this.elements.settingRps.value) || 1.0;
    this.settings.concurrency = parseInt(this.elements.settingConcurrency.value, 10) || 2;
    this.settings.anonymize = this.elements.settingAnonymize.checked;
    this.settings.includeBadges = this.elements.settingIncludeBadges.checked;
    this.settings.bypassCache = this.elements.settingBypassCache.checked;
  }

  startRun() {
    this.readSettings();

    if (this.mode === 'LIVE') {
      this.startLiveRun();
    } else {
      // In DEMO mode on GitHub Pages, dynamically build simulated cohort from uploaded or pasted input
      if (this.activeTab === 'upload' && this.selectedFile) {
        const reader = new FileReader();
        reader.onload = (e) => {
          const text = e.target.result;
          const urls = parseRosterCSV(text);
          if (urls.length > 0) {
            this.startCustomDemoStream(urls);
          } else {
            this.setState('streaming');
          }
        };
        reader.readAsText(this.selectedFile);
      } else if (this.activeTab === 'paste') {
        const text = this.elements.urlsTextarea ? this.elements.urlsTextarea.value.trim() : '';
        const urls = text.split('\n').map(u => u.trim()).filter(u => u.length > 0);
        if (urls.length > 0) {
          this.startCustomDemoStream(urls);
        } else {
          this.setState('streaming');
        }
      } else {
        this.setState('streaming');
      }
    }
  }

  startCustomDemoStream(urls) {
    this.setState('streaming');
    // Map custom URLs to demo results structure
    const customItems = urls.map((u, idx) => {
      const match = u.match(/public_profiles\/([a-f0-9-]+)/i);
      const uuid = match ? match[1] : `demo-uuid-${idx + 1}`;
      const badgesCount = (idx % 4 === 0) ? 10 : (idx % 3 === 0) ? 8 : (idx % 2 === 0) ? 5 : 2;
      const status = badgesCount >= 10 ? 'complete' : (badgesCount > 0 ? 'in_progress' : 'no_badges');
      return {
        student: { name: `Learner ${idx + 1}`, profile_url: u, uuid: uuid },
        display_name: `Learner ${idx + 1}`,
        pseudonym: `Learner-${idx + 1}`,
        status: status,
        counted_badges: badgesCount,
        total_badges: badgesCount,
        badges: Array.from({ length: badgesCount }, (_, bIdx) => ({
          title: `Google Skills Badge ${bIdx + 1}`,
          earned_at: '2026-11-01'
        }))
      };
    });

    this.currentSnapshot = {
      summary: {
        goal: this.settings.goal || 10,
        total_students: customItems.length,
        complete_count: customItems.filter(r => r.status === 'complete').length,
        in_progress_count: customItems.filter(r => r.status === 'in_progress').length,
        no_badges_count: customItems.filter(r => r.status === 'no_badges').length,
        unreachable_count: 0
      },
      results: customItems,
      unreachable: []
    };
    this.applyFilterAndSort();
  }

  async startLiveRun() {
    this.setState('validating');

    try {
      let body;
      const headers = {};

      const settingsPayload = {
        goal: this.settings.goal,
        rps: this.settings.rps,
        concurrency: this.settings.concurrency,
        anonymize: this.settings.anonymize,
        include_badges: this.settings.includeBadges,
        bypass_cache: this.settings.bypassCache
      };

      if (this.activeTab === 'upload') {
        if (!this.selectedFile) {
          alert('Please select a roster CSV file first.');
          this.setState('empty');
          return;
        }
        const fd = new FormData();
        fd.append('file', this.selectedFile);
        fd.append('goal', String(this.settings.goal));
        fd.append('rps', String(this.settings.rps));
        fd.append('concurrency', String(this.settings.concurrency));
        fd.append('anonymize', String(this.settings.anonymize));
        fd.append('include_badges', String(this.settings.includeBadges));
        fd.append('bypass_cache', String(this.settings.bypassCache));
        body = fd;
      } else if (this.activeTab === 'paste') {
        const text = this.elements.urlsTextarea.value.trim();
        const urls = text.split('\n').map(u => u.trim()).filter(u => u.length > 0);
        if (urls.length === 0) {
          alert('Please paste at least one profile URL.');
          this.setState('empty');
          return;
        }
        headers['Content-Type'] = 'application/json';
        body = JSON.stringify({ urls, ...settingsPayload });
      } else if (this.activeTab === 'sheet') {
        const sheetUrl = this.elements.sheetInput.value.trim();
        if (!sheetUrl) {
          alert('Please enter a published Google Sheets CSV URL.');
          this.setState('empty');
          return;
        }
        headers['Content-Type'] = 'application/json';
        body = JSON.stringify({ sheet_url: sheetUrl, ...settingsPayload });
      }

      this.setState('starting');
      if (this.accessCode) {
        headers['Authorization'] = `Bearer ${this.accessCode}`;
      }
      const res = await fetch('/api/runs', { method: 'POST', headers, body });
      if (res.status === 401) {
        const errJson = await res.json().catch(() => ({}));
        this.pendingRunAction = () => this.startLiveRun();
        if (this.elements.accessCodeModal) {
          this.elements.accessCodeModal.classList.add('is-open');
          if (this.elements.accessCodeError) {
            this.elements.accessCodeError.classList.remove('hidden');
            this.elements.accessCodeError.textContent = errJson.error || 'Organizer access code required.';
          }
          if (this.elements.accessCodeInput) {
            this.elements.accessCodeInput.value = '';
            this.elements.accessCodeInput.focus();
          }
        }
        return;
      }
      if (!res.ok) {
        const errJson = await res.json().catch(() => ({}));
        throw new Error(errJson.error || `Server responded with ${res.status}`);
      }

      const data = await res.json();
      this.currentRunId = data.run_id;
      this.subscribeLiveEvents(this.currentRunId);
    } catch (err) {
      console.error('Failed to start run:', err);
      this.setState('error', { error: err.message });
    }
  }

  subscribeLiveEvents(runId) {
    this.setState('streaming');
    if (this.eventSource) {
      this.eventSource.close();
    }
    this.eventSource = new EventSource(`/api/runs/${runId}/events`);

    this.liveResults = [];
    this.liveUnreachable = [];

    this.eventSource.addEventListener('progress', (e) => {
      const stats = JSON.parse(e.data);
      this.renderProgressStripUI(stats);
    });

    this.eventSource.addEventListener('row_result', (e) => {
      const row = JSON.parse(e.data);
      if (row.status === 'private_or_not_found' || row.status === 'network_error' || row.status === 'rate_limited' || row.status === 'parse_error') {
        this.liveUnreachable.push(row);
      } else {
        this.liveResults.push(row);
      }
      this.currentSnapshot = {
        summary: {
          goal: this.settings.goal,
          total_students: this.liveResults.length + this.liveUnreachable.length,
          complete_count: this.liveResults.filter(r => r.status === 'complete').length
        },
        results: this.liveResults,
        unreachable: this.liveUnreachable
      };
      this.applyFilterAndSort();
    });

    this.eventSource.addEventListener('run_complete', (e) => {
      this.eventSource.close();
      const snapshot = JSON.parse(e.data);
      if (this.previousSnapshot) {
        const prevComplete = this.previousSnapshot.summary?.complete_count || 0;
        const currComplete = snapshot.summary?.complete_count || 0;
        this.sessionComparisonDiff = currComplete - prevComplete;
      } else {
        this.sessionComparisonDiff = null;
      }
      this.previousSnapshot = JSON.parse(JSON.stringify(snapshot));
      this.currentSnapshot = snapshot;
      this.setState('complete');
    });

    this.eventSource.addEventListener('run_cancelled', () => {
      this.eventSource.close();
      this.setState('cancelled');
    });

    this.eventSource.addEventListener('error', () => {
      this.eventSource.close();
      this.setState('error', { error: 'Lost connection to live event stream.' });
    });
  }

  cancelCurrentRun() {
    if (this.mode === 'LIVE' && this.currentRunId) {
      const headers = {};
      if (this.accessCode) headers['Authorization'] = `Bearer ${this.accessCode}`;
      fetch(`/api/runs/${this.currentRunId}/cancel`, { method: 'POST', headers }).catch(console.error);
    }
    if (this.streamingInterval) {
      clearInterval(this.streamingInterval);
      this.streamingInterval = null;
    }
    this.setState('cancelled');
  }

  retryUnreachable() {
    if (this.mode === 'LIVE' && this.currentRunId) {
      const headers = {};
      if (this.accessCode) headers['Authorization'] = `Bearer ${this.accessCode}`;
      fetch(`/api/runs/${this.currentRunId}/retry`, { method: 'POST', headers })
        .then((res) => {
          if (res.status === 401) {
            this.pendingRunAction = () => this.retryUnreachable();
            if (this.elements.accessCodeModal) {
              this.elements.accessCodeModal.classList.add('is-open');
            }
            return;
          }
          this.subscribeLiveEvents(this.currentRunId);
        })
        .catch(console.error);
    } else {
      // Demo retry simulation: resolves network/rate-limit profiles into completed rows
      alert('Demo mode: Retrying transient errors...');
      this.setState('complete');
    }
  }

  setState(newState, extra = {}) {
    this.state = newState;
    if (this.streamingInterval) {
      clearInterval(this.streamingInterval);
      this.streamingInterval = null;
    }

    // Highlight active dev button
    document.querySelectorAll('.dev-btn').forEach(btn => {
      btn.classList.toggle('active', btn.dataset.state === newState);
    });

    // Clear dynamic containers
    this.elements.progressStripContainer.textContent = '';
    this.elements.summaryContainer.textContent = '';
    this.elements.distContainer.textContent = '';
    this.elements.tableContainer.textContent = '';
    this.elements.unreachableContainer.textContent = '';
    this.elements.reportStripContainer.textContent = '';
    this.elements.stateMessageContainer.textContent = '';

    const showResults = (show) => {
      if (show) {
        this.elements.inputSection?.classList.add('hidden');
        this.elements.resultsSection?.classList.remove('hidden');
      } else {
        this.elements.inputSection?.classList.remove('hidden');
        this.elements.resultsSection?.classList.add('hidden');
      }
    };

    switch (newState) {
      case 'empty':
        showResults(false);
        this.elements.toolbarSection?.classList.add('hidden');
        break;

      case 'validating':
        showResults(true);
        this.elements.toolbarSection?.classList.add('hidden');
        this.elements.stateMessageContainer.appendChild(
          renderStateMessage('Validating Roster', 'Verifying URL formats and parsing participant records...')
        );
        break;

      case 'starting':
        showResults(true);
        this.elements.toolbarSection?.classList.add('hidden');
        this.elements.stateMessageContainer.appendChild(
          renderStateMessage('Initiating Run', 'Connecting to upstream evaluator and establishing streaming session...')
        );
        break;

      case 'streaming':
        showResults(true);
        this.elements.toolbarSection?.classList.remove('hidden');
        this.startSimulatedStreaming();
        break;

      case 'complete':
        showResults(true);
        this.elements.toolbarSection?.classList.remove('hidden');
        this.currentSnapshot = JSON.parse(JSON.stringify(DEMO_DATA));
        this.applyFilterAndSort();
        break;

      case 'partial':
        showResults(true);
        this.elements.toolbarSection?.classList.remove('hidden');
        this.currentSnapshot = JSON.parse(JSON.stringify(DEMO_DATA));
        this.applyFilterAndSort();
        break;

      case 'cancelled':
        showResults(true);
        this.elements.toolbarSection?.classList.remove('hidden');
        this.currentSnapshot = {
          summary: { goal: 10, total_students: 8, complete_count: 2 },
          results: DEMO_DATA.results.slice(0, 6),
          unreachable: DEMO_DATA.unreachable.slice(0, 2)
        };
        this.elements.progressStripContainer.appendChild(
          renderStateMessage('Run Cancelled', 'The evaluation run was stopped early by the operator. Partial results are preserved below.')
        );
        this.applyFilterAndSort();
        break;

      case 'error':
        showResults(true);
        this.elements.toolbarSection?.classList.add('hidden');
        this.elements.stateMessageContainer.appendChild(
          renderStateMessage('Evaluation Error', extra.error || 'Server communication failure or network timeout. Please check server status.', true)
        );
        break;

      case 'expired':
        showResults(true);
        this.elements.toolbarSection?.classList.add('hidden');
        this.elements.stateMessageContainer.appendChild(
          renderStateMessage('Run Expired', 'This in-memory run session has passed its retention TTL (30 minutes). Please start a new run.')
        );
        break;

      case 'zero':
        showResults(true);
        this.elements.toolbarSection?.classList.remove('hidden');
        this.currentSnapshot = {
          summary: { goal: 10, total_students: 5, complete_count: 0, no_badges_count: 5 },
          results: DEMO_DATA.results.filter(r => r.status === 'no_badges').concat(
            DEMO_DATA.results.slice(0, 3).map(r => ({ ...r, status: 'no_badges', counted_badges: 0, badges: [] }))
          ),
          unreachable: []
        };
        this.applyFilterAndSort();
        break;

      case 'huge':
        showResults(true);
        this.elements.toolbarSection?.classList.remove('hidden');
        this.currentSnapshot = generateLargeDemoDataset(250);
        this.applyFilterAndSort();
        break;
    }
  }

  startSimulatedStreaming() {
    const full = JSON.parse(JSON.stringify(DEMO_DATA));
    const allItems = [...full.results, ...full.unreachable];
    let currentIndex = 0;

    this.currentSnapshot = {
      summary: { goal: 10, total_students: allItems.length, complete_count: 0 },
      results: [],
      unreachable: []
    };

    const renderTick = () => {
      if (currentIndex >= allItems.length) {
        clearInterval(this.streamingInterval);
        this.streamingInterval = null;
        if (this.previousSnapshot) {
          const prevComplete = this.previousSnapshot.summary?.complete_count || 0;
          const currComplete = this.currentSnapshot.summary?.complete_count || 0;
          this.sessionComparisonDiff = currComplete - prevComplete;
        } else {
          this.sessionComparisonDiff = null;
        }
        this.previousSnapshot = JSON.parse(JSON.stringify(this.currentSnapshot));
        this.setState('complete');
        return;
      }

      const item = allItems[currentIndex];
      if (item.status === 'private_or_not_found' || item.status === 'network_error' || item.status === 'rate_limited') {
        this.currentSnapshot.unreachable.push(item);
      } else {
        this.currentSnapshot.results.push(item);
        if (item.status === 'complete') {
          this.currentSnapshot.summary.complete_count++;
        }
      }

      currentIndex++;
      const stats = {
        done: currentIndex,
        total: allItems.length,
        rate: 1.0,
        cached: Math.floor(currentIndex * 0.4),
        eta: `${allItems.length - currentIndex}s`,
        isPaused: currentIndex === 7
      };

      this.renderProgressStripUI(stats);
      this.applyFilterAndSort();
    };

    renderTick();
    this.streamingInterval = setInterval(renderTick, 650);
  }

  renderProgressStripUI(stats) {
    this.elements.progressStripContainer.textContent = '';
    this.elements.progressStripContainer.appendChild(
      renderProgressStrip(stats, () => this.cancelCurrentRun())
    );
  }

  applyFilterAndSort() {
    if (!this.currentSnapshot) return;

    const rawResults = this.currentSnapshot.results || [];
    let list = [...rawResults];

    // Status filter
    if (this.activeFilter !== 'all') {
      if (this.activeFilter === 'unreachable') {
        list = [];
      } else {
        list = list.filter(r => r.status === this.activeFilter);
      }
    }

    // Search query (learner name, pseudonym, profile URL, UUID)
    if (this.searchQuery) {
      list = list.filter(r => {
        const name = (r.display_name || '').toLowerCase();
        const pseudo = (r.pseudonym || '').toLowerCase();
        const url = (r.student && r.student.profile_url ? r.student.profile_url : '').toLowerCase();
        const uuid = (r.student && r.student.uuid ? r.student.uuid : '').toLowerCase();
        return name.includes(this.searchQuery) || pseudo.includes(this.searchQuery) || url.includes(this.searchQuery) || uuid.includes(this.searchQuery);
      });
    }

    // Sort
    list.sort((a, b) => {
      const nameA = (a.display_name || a.pseudonym || '').toLowerCase();
      const nameB = (b.display_name || b.pseudonym || '').toLowerCase();

      switch (this.sortBy) {
        case 'progress-desc':
          if ((b.counted_badges || 0) !== (a.counted_badges || 0)) {
            return (b.counted_badges || 0) - (a.counted_badges || 0);
          }
          return nameA.localeCompare(nameB);

        case 'progress-asc':
          if ((a.counted_badges || 0) !== (b.counted_badges || 0)) {
            return (a.counted_badges || 0) - (b.counted_badges || 0);
          }
          return nameA.localeCompare(nameB);

        case 'name-asc':
          return nameA.localeCompare(nameB);

        case 'date-desc': {
          const dateA = a.last_badge_date || '0000-00-00';
          const dateB = b.last_badge_date || '0000-00-00';
          if (dateB !== dateA) return dateB.localeCompare(dateA);
          return (b.counted_badges || 0) - (a.counted_badges || 0);
        }

        default:
          return 0;
      }
    });

    this.filteredResults = list;
    this.renderResultsView();
  }

  renderResultsView() {
    const snapshot = this.currentSnapshot;
    if (!snapshot) return;

    const summary = snapshot.summary || {};
    const goal = summary.goal || 10;
    const completeCount = summary.complete_count || (snapshot.results || []).filter(r => r.status === 'complete').length;
    const totalCount = summary.total_students || ((snapshot.results || []).length + (snapshot.unreachable ? snapshot.unreachable.length : 0));

    const unreachableList = snapshot.unreachable || [];
    const isBlocked = ((snapshot.results || []).length === 0 && unreachableList.length > 0);

    // Update Last Checked timestamp badge
    if (this.elements.lastCheckedBadge) {
      const now = new Date();
      const timeStr = now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
      this.elements.lastCheckedBadge.textContent = `Checked at ${timeStr}`;
      this.elements.lastCheckedBadge.className = 'last-checked-badge fresh';
    }

    // A. Render BADGE ARENA View
    if (this.elements.arenaViewContainer) {
      this.elements.arenaViewContainer.textContent = '';
      this.elements.arenaViewContainer.appendChild(
        renderArenaView(snapshot.results || [], {
          goal: goal,
          anonymize: this.settings.anonymize,
          prevRankMap: this.prevRankMap
        })
      );
    }

    // B. Render Editorial Ledger View
    // 1. Summary Headline
    if (this.elements.summaryContainer) {
      this.elements.summaryContainer.textContent = '';
      this.elements.summaryContainer.appendChild(
        renderSummarySentence(completeCount, totalCount, goal, this.sessionComparisonDiff, isBlocked)
      );
    }

    // 2. Distribution Bar
    if (this.elements.distContainer) {
      const dist = computeDistribution(snapshot.results || [], goal);
      this.elements.distContainer.textContent = '';
      this.elements.distContainer.appendChild(renderDistributionBar(dist, (snapshot.results || []).length));
    }

    // 3. Roster Table
    if (this.elements.tableContainer) {
      this.elements.tableContainer.textContent = '';
      this.elements.tableContainer.appendChild(
        renderRosterTable(this.filteredResults, {
          anonymized: this.settings.anonymize,
          showBadges: this.settings.includeBadges,
          goal: goal
        })
      );
    }

    // 4. Unreachable Section
    if (this.elements.unreachableContainer) {
      this.elements.unreachableContainer.textContent = '';
      if (this.activeFilter === 'all' || this.activeFilter === 'unreachable') {
        this.elements.unreachableContainer.appendChild(
          renderUnreachableSection(unreachableList, () => this.retryUnreachable())
        );
      }
    }

    // 5. Run Report Strip
    if (this.elements.reportStripContainer) {
      this.elements.reportStripContainer.textContent = '';
      this.elements.reportStripContainer.appendChild(renderReportStrip(summary));
    }
  }

  exportCSV() {
    if (!this.currentSnapshot) return;
    const csvContent = generateCSVExport(this.currentSnapshot.results || [], this.settings.anonymize);
    this.triggerDownload('badgeboard_results.csv', 'text/csv;charset=utf-8;', '\uFEFF' + csvContent);
  }

  exportJSON() {
    if (!this.currentSnapshot) return;
    const jsonContent = JSON.stringify(this.currentSnapshot, null, 2);
    this.triggerDownload('badgeboard_results.json', 'application/json', jsonContent);
  }

  exportReportHTML() {
    if (!this.currentSnapshot) return;
    const htmlContent = generateOfflineReport(this.currentSnapshot, { anonymized: this.settings.anonymize });
    this.triggerDownload('badgeboard_report.html', 'text/html;charset=utf-8;', htmlContent);
  }

  triggerDownload(filename, mimeType, content) {
    const blob = new Blob([content], { type: mimeType });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
}

// Instantiate and mount application
window.addEventListener('DOMContentLoaded', () => {
  const app = new BadgeboardApp();
  app.init().catch(console.error);
});
