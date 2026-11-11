// Pure CSV utilities for badgeboard: parsing, formula injection neutralization, and generation.

/**
 * Neutralizes Excel / Google Sheets formula injection (DDE attacks).
 * Any cell value starting with '=', '+', '-', '@', tab, or carriage return
 * is prepended with a single quote (').
 */
export function sanitizeCSVCell(value) {
  if (value === null || value === undefined) {
    return '';
  }
  const str = String(value);
  if (str.length === 0) {
    return '';
  }

  const firstChar = str.charAt(0);
  if (firstChar === '=' || firstChar === '+' || firstChar === '-' || firstChar === '@' || firstChar === '\t' || firstChar === '\r') {
    return "'" + str;
  }
  return str;
}

/**
 * Escapes a cell for RFC 4180 CSV compliance after formula neutralization.
 */
export function escapeCSVCell(value) {
  const sanitized = sanitizeCSVCell(value);
  if (sanitized.includes('"') || sanitized.includes(',') || sanitized.includes('\n') || sanitized.includes('\r')) {
    return `"${sanitized.replace(/"/g, '""')}"`;
  }
  return sanitized;
}

/**
 * Parses raw CSV text into an array of profile URLs.
 * Recognizes "Google Skills Profile URL" or "Profile URL" header,
 * or treats a single-column file as profile URLs directly.
 */
export function parseRosterCSV(text) {
  if (!text || typeof text !== 'string') {
    return { urls: [], badRows: 0 };
  }

  // Strip UTF-8 BOM if present
  let cleanText = text;
  if (cleanText.charCodeAt(0) === 0xFEFF) {
    cleanText = cleanText.slice(1);
  }

  const lines = cleanText.split(/\r\n|\n|\r/);
  const rows = [];
  for (const line of lines) {
    const trimmed = line.trim();
    if (trimmed.length > 0) {
      rows.push(parseCSVLine(trimmed));
    }
  }

  if (rows.length === 0) {
    return { urls: [], badRows: 0 };
  }

  // Detect header column
  const header = rows[0].map(h => h.trim().toLowerCase());
  let targetCol = -1;

  for (let i = 0; i < header.length; i++) {
    const h = header[i];
    if (h === 'google skills profile url' || h === 'profile url' || h.includes('profile url')) {
      targetCol = i;
      break;
    }
  }

  let startIndex = 1;
  if (targetCol === -1) {
    // Single column or header-less
    if (header.length === 1 && !header[0].startsWith('http')) {
      // It's a header like "URL" or "Links"
      targetCol = 0;
      startIndex = 1;
    } else {
      // First row might be an actual URL
      targetCol = 0;
      startIndex = 0;
    }
  }

  const urls = [];
  let badRows = 0;

  for (let i = startIndex; i < rows.length; i++) {
    const row = rows[i];
    if (!row || row.length <= targetCol) {
      badRows++;
      continue;
    }
    const val = row[targetCol].trim();
    if (val.length === 0) {
      continue;
    }
    // Basic verification: must look like a URL
    if (val.startsWith('http://') || val.startsWith('https://')) {
      urls.push(val);
    } else {
      badRows++;
    }
  }

  return { urls, badRows };
}

/**
 * Simple CSV line parser respecting quoted fields.
 */
export function parseCSVLine(line) {
  const result = [];
  let current = '';
  let inQuotes = false;

  for (let i = 0; i < line.length; i++) {
    const char = line[i];
    if (char === '"') {
      if (inQuotes && i + 1 < line.length && line[i + 1] === '"') {
        current += '"';
        i++; // skip escaped quote
      } else {
        inQuotes = !inQuotes;
      }
    } else if (char === ',' && !inQuotes) {
      result.push(current);
      current = '';
    } else {
      current += char;
    }
  }
  result.push(current);
  return result;
}

/**
 * Generates an RFC 4180 CSV export string with formula injection neutralization.
 */
export function generateCSVExport(records, isAnonymized = false) {
  const header = ['Learner', 'Status', 'Counted Badges', 'Goal', 'Last Badge Date'];
  const lines = [header.map(escapeCSVCell).join(',')];

  for (const r of records) {
    const name = isAnonymized ? (r.pseudonym || 'Learner-XXXX') : (r.display_name || (r.student && r.student.name) || 'Anonymous');
    const status = r.status || 'unknown';
    const counted = r.counted_badges !== undefined ? r.counted_badges : 0;
    const goal = r.goal || 10;
    const lastDate = r.last_badge_date || '';

    const row = [
      escapeCSVCell(name),
      escapeCSVCell(status),
      escapeCSVCell(counted),
      escapeCSVCell(goal),
      escapeCSVCell(lastDate)
    ];
    lines.push(row.join(','));
  }

  return lines.join('\r\n') + '\r\n';
}
