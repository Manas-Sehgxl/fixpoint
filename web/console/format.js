// Words and numbers: durations, ages, and what each status means in plain
// English. Kept in one place so the board, the case file and the status page
// say the same thing the same way.

export function duration(sec) {
  if (!Number.isFinite(sec) || sec < 0) return '';
  const s = Math.round(sec);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, '0')}s`;
  const hrs = Math.floor(m / 60);
  if (hrs < 48) return `${hrs}h ${String(m % 60).padStart(2, '0')}m`;
  return `${Math.floor(hrs / 24)}d`;
}

export function since(iso, now = Date.now()) {
  return Math.max(0, (now - Date.parse(iso)) / 1000);
}

export function ago(iso, now = Date.now()) {
  const s = since(iso, now);
  if (s < 45) return 'just now';
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))}m ago`;
  if (s < 86400) return `${Math.round(s / 3600)}h ago`;
  if (s < 7 * 86400) return `${Math.round(s / 86400)}d ago`;
  return dateShort(iso);
}

export function dateShort(iso) {
  return new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
}

export function clock(iso) {
  return new Date(iso).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

export function dateTime(iso) {
  const d = new Date(iso);
  return `${d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })} at ${d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })}`;
}

export function plural(n, one, many = `${one}s`) {
  return `${n} ${n === 1 ? one : many}`;
}

const WORDS = ['no', 'one', 'two', 'three', 'four', 'five', 'six', 'seven', 'eight', 'nine', 'ten'];
export function countWord(n) { return WORDS[n] || String(n); }

export function ordinal(n) {
  const v = n % 100;
  const suf = v >= 11 && v <= 13 ? 'th' : ({ 1: 'st', 2: 'nd', 3: 'rd' }[n % 10] || 'th');
  return `${n}${suf}`;
}

export function credits(n) {
  if (!n) return '0';
  return n >= 100 ? String(Math.round(n)) : n.toFixed(1);
}

export function usd(n) {
  return `$${(n || 0).toFixed(2)}`;
}

/** status words for a report: short for rows, with the attempt where it helps. */
export function statusText(r) {
  const att = r.attempts ? (Array.isArray(r.attempts) ? r.attempts.length : r.attempts) : 0;
  switch (r.status) {
    case 'queued': return r.queue_position > 1 ? `In line, ${ordinal(r.queue_position)}` : 'Next in line';
    case 'triaging': return 'Writing the check';
    case 'reproducing': return 'Reproducing';
    case 'needs_info': return 'Waiting for the reporter';
    case 'awaiting_approval': return 'Waiting for your approval';
    case 'fixing': return `Fixing, attempt ${att || 1}`;
    case 'verifying': return `Verifying attempt ${att || 1}`;
    case 'regressing': return 'Re-checking earlier fixes';
    case 'proven': return 'Proven';
    case 'unreproduced': return 'Not reproduced';
    case 'escalated': return 'Handed to a person';
    case 'error': return 'Stopped by an error';
    default: return r.status;
  }
}

/** stage words for the live panel: a full sentence. */
export function stageSentence(r) {
  const att = Array.isArray(r.attempts) ? r.attempts.length : r.attempts;
  switch (r.status) {
    case 'queued': return 'Waiting for the worker.';
    case 'triaging': return 'Claude Code is reading the report and writing a check.';
    case 'reproducing': return 'Kane is running the check in a real browser to see the bug.';
    case 'fixing': return `Claude Code is writing attempt ${att || 1} of the fix.`;
    case 'verifying': return 'Kane is running the same check against the patched app.';
    case 'regressing': return 'Kane is re-running the checks of earlier fixes.';
    default: return statusText(r) + '.';
  }
}

export const ACTIVE = ['queued', 'triaging', 'reproducing', 'fixing', 'verifying', 'regressing'];
export const WORKING = ['triaging', 'reproducing', 'fixing', 'verifying', 'regressing'];
export const NEEDS_PERSON = ['needs_info', 'awaiting_approval', 'escalated', 'error'];
export const DONE = ['proven', 'unreproduced', 'escalated'];

/** markFor maps a report status to the plot mark that stands for it. */
export function markFor(status) {
  if (status === 'proven') return 'passed';
  if (status === 'queued') return 'pending';
  if (WORKING.includes(status)) return 'active';
  if (status === 'needs_info' || status === 'awaiting_approval') return 'waiting';
  if (status === 'escalated' || status === 'error') return 'failed';
  return 'done';
}

/** browser turns a user agent string into "Chrome 131 on macOS". */
export function browser(ua) {
  if (!ua) return '';
  const os = /iPhone|iPad/.test(ua) ? 'iOS' : /Android/.test(ua) ? 'Android' : /Mac OS X/.test(ua) ? 'macOS' : /Windows/.test(ua) ? 'Windows' : /Linux/.test(ua) ? 'Linux' : '';
  const m = ua.match(/(Edg|Firefox|Chrome|Version)\/(\d+)/);
  let name = m ? { Edg: 'Edge', Firefox: 'Firefox', Chrome: 'Chrome', Version: 'Safari' }[m[1]] : '';
  if (name === 'Chrome' && /iPhone|iPad/.test(ua)) name = 'Chrome';
  return [name && `${name} ${m[2]}`, os && `on ${os}`].filter(Boolean).join(' ') || ua.slice(0, 60);
}

/**
 * toolName is a tool's name without a trailing "(simulated)": the engine puts
 * it there, and the console says that in its own words.
 */
export function toolName(name) {
  return String(name || '').replace(/\s*\(simulated\)\s*$/i, '');
}

/**
 * reasonText turns Kane's machine reason into a sentence. Kane writes
 * "assertion_failed: @ step 7"; everything else is shown as it came.
 */
export function reasonText(reason) {
  const r = String(reason || '').trim();
  const m = r.match(/^assertion_failed:?\s*@?\s*step\s+(\d+)$/i);
  if (m) return `The assertion at step ${m[1]} did not hold.`;
  if (/^assertion_failed$/i.test(r)) return 'The final assertion did not hold.';
  return humanize(r);
}

export function humanize(s) {
  const t = String(s || '').replace(/_/g, ' ');
  return t.charAt(0).toUpperCase() + t.slice(1);
}
