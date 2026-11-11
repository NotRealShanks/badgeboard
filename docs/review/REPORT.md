# Quality Loop & Design Verification Report (Phase W1)

## 1. Overview
This report documents the design verification, accessibility audits, and forbidden-list inspection for the "editorial ledger" web application prototype across all required states, viewports, and color schemes.

---

## 2. Forbidden Design Checks
Every generated screenshot and layout was audited against the project's strict forbidden elements list:

| Rule | Status | Implementation Details |
|---|---|---|
| **No purple/blue gradients** | PASS | Paper-toned background (`#F6F1E7` light, `#14120F` dark) with single vermilion accent (`#BA3E18`). |
| **No glassmorphism** | PASS | Solid opaque backgrounds with hairline borders (`#DDD6C9` / `#2E2923`); zero `backdrop-filter: blur`. |
| **No generic stat-tile rows** | PASS | Custom typographic summary sentence ("X of Y learners have completed all 10 badges") plus segmented distribution bar. |
| **No identical rounded cards in a grid** | PASS | Tabular ledger table layout with hairline divider rules; responsive stacked rows on mobile. |
| **No emojis in headings** | PASS | Pure editorial typography using Fraunces and JetBrains Mono; status uses symbols `[✓]`, `[>]`, `[-]`, `[?]`, `[!]`. |
| **No pie or donut charts** | PASS | Horizontal 5-bucket distribution ledger bar (0, 1–3, 4–6, 7–9, 10). |
| **No decorative blobs** | PASS | Zero abstract SVG background blobs or ambient gradient meshes. |
| **No system-font-only styling** | PASS | Self-hosted OFL fonts: **Fraunces** (editorial serif) and **JetBrains Mono** (monospace data). |
| **No avatars** | PASS | Typographic names or cryptographically derived pseudonyms (`Learner-XXXX`). |
| **No rank numbers, podiums, or medals** | PASS | Non-gamified roster list; default sort by progress descending; zero 1st/2nd/3rd badges or trophies. |
| **No drop-shadow cards** | PASS | Flat 1px hairline rules throughout (`#DDD6C9` / `#2E2923`). |

---

## 3. Automated Accessibility Audit (axe-core)
Audited via headless Chromium CDP WebSocket using `scratch/test_axe.js`:

| Viewport & Theme | Axe Passes | Axe Violations | Critical / Serious Issues | Status |
|---|---|---|---|---|
| **Desktop Light** (1280×900) | 28 | 0 | 0 | PASS |
| **Desktop Dark** (1280×900) | 28 | 0 | 0 | PASS |
| **Mobile 360px Light** (360×800) | 29 | 0 | 0 | PASS |
| **Mobile 360px Dark** (360×800) | 29 | 0 | 0 | PASS |

### Iterations & Contrast Fixes:
1. `#modeTag` contrast in light mode: adjusted warning amber to `#92400E` (> 5:1 contrast).
2. `.dist-title` contrast in light and dark modes: adjusted to `var(--ink-secondary)` (`#4D4942` light, `#BAAF9F` dark, > 5.5:1 contrast).
3. `.dist-bucket-7-9` and `.dist-bucket-10` text contrast: adjusted text to `var(--ink)` / `var(--bg)` ensuring > 7:1 contrast on accent blocks.

---

## 4. Frontend Pure Unit Tests (`node --test`)
Executed `node --test internal/web/assets/frontend.test.js`:
- `sanitizeCSVCell: neutralizes formula injection`: **PASS**
- `escapeCSVCell: quotes cells with commas, quotes, and newlines`: **PASS**
- `parseRosterCSV: parses valid profile URLs from CSV`: **PASS**
- `parseRosterCSV: handles single-column CSV without headers or custom header`: **PASS**
- `generateCSVExport: outputs valid neutralized CSV`: **PASS**
- `computeDistribution: buckets counts accurately`: **PASS**

Total: **6 passed, 0 failed**.

---

## 5. Captured Review Screenshots (`docs/review/`)
All captured states contain strictly fictional synthetic demo data:
- `w1_empty_desktop_light.png` / `w1_empty_desktop_dark.png`
- `w1_empty_mobile360_light.png` / `w1_empty_mobile360_dark.png`
- `w1_streaming_desktop_light.png` / `w1_streaming_desktop_dark.png`
- `w1_streaming_mobile360_light.png` / `w1_streaming_mobile360_dark.png`
- `w1_complete_desktop_light.png` / `w1_complete_desktop_dark.png`
- `w1_complete_mobile360_light.png` / `w1_complete_mobile360_dark.png`
- `w1_partial_desktop_light.png` / `w1_partial_desktop_dark.png`
- `w1_partial_mobile360_light.png` / `w1_partial_mobile360_dark.png`
- `w1_error_desktop_light.png` / `w1_error_desktop_dark.png`
- `w1_error_mobile360_light.png` / `w1_error_mobile360_dark.png`

---

## 6. Live API Wiring & Streaming Verification (Phase W3)
The frontend application was wired directly to the live backend server:
- **Server Health Detection**: On startup, `detectMode()` probes `GET /healthz`. In the live Go server environment, it detects HTTP 200 and sets status tag to `Live Backend`.
- **Input Methods**:
  - `Upload CSV`: Dispatches multipart/form-data to `POST /api/runs`.
  - `Paste URLs`: Formats JSON payload `{ urls, ...settings }` with snake_case parameter serialization.
  - `Google Sheet link`: Formats JSON payload `{ sheet_url, ...settings }`.
- **Live EventSource Streaming**:
  - Subscribes to `/api/runs/{id}/events` via Server-Sent Events (SSE).
  - Listens to `progress`, `row_result`, `run_complete`, `run_cancelled`, and `error` events.
  - Dynamically updates the 10-segment progress bar, distribution breakdown, and roster rows as results stream in.
- **Run Controls**:
  - `Cancel`: Dispatches `POST /api/runs/{id}/cancel` to immediately halt the runner and broadcast cancellation.
  - `Retry unreachable`: Dispatches `POST /api/runs/{id}/retry` to re-fetch only transient network/rate-limited errors without re-scraping private or completed profiles.
- **Session Comparison (Stretch Feature)**:
  - Preserves prior run snapshot in the browser session.
  - Upon run completion, calculates differential `(+N completed since last run)` or `(no change since last run)` and renders it alongside the Fraunces summary headline.

