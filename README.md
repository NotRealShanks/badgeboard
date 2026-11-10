# Google Skills Badgeboard

A resilient, privacy-first progress tracking engine and static web dashboard for Google Developer Groups (GDG) "Learn With Communities" campaign.

---

## Key Capabilities

- **Polite & Adaptive Scraping**: Global rate limiting (token bucket) with concurrent worker pool, adaptive cool-down on `429 Too Many Requests` / `503 Service Unavailable`, and server `Retry-After` header adherence.
- **Smart Disk Caching**: Status-based TTL caching (6-hour TTL for successful `200 OK` profiles, 15-minute TTL for `PrivateOrNotFound`, zero caching for transient network/rate-limit errors).
- **Two Data Privacy Tiers**:
  - **Public Anonymized Tier (`--anonymize`)**: Generates irreversible HMAC-SHA256 pseudonyms (`Learner-XXXX`) keyed with an out-of-band secret salt. Strictly omits badge catalogs, leagues, points, URLs, UUIDs, and full-precision timestamps to eliminate quasi-identifier linkage.
  - **Detailed Anonymized Tier (`--anonymize --include-badges`)**: Retains badge titles and dates for expandable details with URLs scrubbed.
- **Formula Injection Defense**: Automatically neutralizes CSV injection (`=`, `+`, `-`, `@`, `\t`, `\r`) in both Go CLI exports and client-side web exports.
- **Static Web Dashboard (`docs/`)**:
  - Pure vanilla HTML5/CSS3/JavaScript (zero frameworks, zero bundlers, zero runtime dependencies).
  - Strict Content Security Policy (`default-src 'self' ...`).
  - Self-hosted web fonts: **Fraunces** (editorial serif headings) and **JetBrains Mono** (data/metrics) with OFL licenses.
  - Zero `innerHTML`: All dynamic rendering utilizes `.textContent` and programmatic DOM nodes.
  - Fully responsive from high-resolution desktop down to 360px mobile viewports.
  - 100/100 Lighthouse Accessibility score and 0 axe-core violations across light and dark themes.

---

## Data Privacy & Public Deployment Policy

> [!IMPORTANT]
> **Organizer Authorization Required**: Publishing real participant progress data to any public or web-facing medium requires explicit written permission from the GDG campaign organizers.

- The public dashboard hosted on GitHub Pages (`docs/`) serves synthetic demo data exclusively, generated via `badgeboard demo`.
- The real-roster synchronization workflow (`.github/workflows/sync-real-roster.yml.disabled`) is disabled by default, requiring repository administrator opt-in (`ENABLE_REAL_ROSTER_SYNC=true`), an encrypted roster secret, and a secret HMAC salt.

---

## Installation & CLI Usage

### Build from Source
```bash
go build -o bin/badgeboard ./cmd/badgeboard
```

### Commands

#### 1. Generate Synthetic Demo Data
```bash
# Generate deterministic demo dataset
badgeboard demo --out docs/data/results.json --format json
```

#### 2. Run Progress Tracking Against a Roster
```bash
# Evaluate a student roster CSV
badgeboard run students.csv --goal 10

# Export anonymized public dataset (requires BADGEBOARD_SALT or --salt-file)
export BADGEBOARD_SALT="your-secret-salt-key"
badgeboard run students.csv --anonymize --format json > public_results.json
```

#### 3. Inspect a Single Profile
```bash
badgeboard check https://www.skills.google/public_profiles/11111111-2222-3333-4444-555555555555
```

#### 4. Cache Management
```bash
# View cache directory and status
badgeboard cache

# Clear all cached profile entries
badgeboard cache clear
```

---

## Testing & Quality Assurance

```bash
# Run unit and race-condition test suite
go test -race -cover ./...

# Run client-side CSV formula neutralization test
node docs/csv_export.test.js

# Run static analysis
golangci-lint run ./...
```
