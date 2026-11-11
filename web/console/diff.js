// Unified diff parsing and the per-file diff viewer. Patches come from
// FileDiff.patch as written by the engine: "--- a/path", "+++ b/path", then
// hunks with three lines of context.

import { h } from './dom.js';

/** parse reads one file's unified diff into hunks of typed lines. */
export function parse(patch) {
  const out = { oldPath: '', newPath: '', hunks: [] };
  let hunk = null, o = 0, n = 0;
  for (const raw of String(patch || '').split('\n')) {
    if (!hunk && raw.startsWith('--- ')) { out.oldPath = raw.slice(4).replace(/^a\//, ''); continue; }
    if (!hunk && raw.startsWith('+++ ')) { out.newPath = raw.slice(4).replace(/^b\//, ''); continue; }
    const m = raw.match(/^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$/);
    if (m) {
      o = Number(m[1]); n = Number(m[3]);
      hunk = { header: raw, context: m[5] || '', oldStart: o, newStart: n, lines: [] };
      out.hunks.push(hunk);
      continue;
    }
    if (!hunk) continue;
    const c = raw.charAt(0), text = raw.slice(1);
    if (c === '+') hunk.lines.push({ type: 'add', text, new: n++ });
    else if (c === '-') hunk.lines.push({ type: 'del', text, old: o++ });
    else if (c === ' ') hunk.lines.push({ type: 'ctx', text, old: o++, new: n++ });
    else if (c === '\\') hunk.lines.push({ type: 'meta', text: raw.slice(2) });
    else if (raw === '') hunk.lines.push({ type: 'blank' });
  }
  for (const hk of out.hunks) {
    while (hk.lines.length && hk.lines[hk.lines.length - 1].type === 'blank') hk.lines.pop();
    for (const l of hk.lines) if (l.type === 'blank') { l.type = 'ctx'; l.text = ''; }
  }
  // Number blank context lines correctly: they were skipped above.
  for (const hk of out.hunks) {
    let oo = hk.oldStart, nn = hk.newStart;
    for (const l of hk.lines) {
      if (l.type === 'ctx') { l.old = oo++; l.new = nn++; }
      else if (l.type === 'del') l.old = oo++;
      else if (l.type === 'add') l.new = nn++;
    }
  }
  return out;
}

const CHANGE = { modified: 'Modified', added: 'Added', deleted: 'Deleted' };

/**
 * fileDiff renders one FileDiff as a collapsible table with old and new line
 * numbers. state is a Map that remembers which files are open across renders.
 */
export function fileDiff(file, key, state) {
  const parsed = parse(file.patch);
  const open = state && state.has(key) ? state.get(key) : true;
  const details = h('details', { class: 'diff', open });
  details.addEventListener('toggle', () => state && state.set(key, details.open));
  const counts = h('span', { class: 'diff-counts' },
    h('span', { class: 'diff-add-count' }, `+${file.additions}`),
    h('span', { class: 'diff-del-count' }, `−${file.deletions}`),
    h('span', { class: 'visually-hidden' }, `: ${file.additions} lines added, ${file.deletions} removed`));
  details.append(h('summary', null,
    h('span', { class: 'diff-path' }, file.path),
    h('span', { class: 'diff-change' }, CHANGE[file.change] || file.change),
    counts));

  const table = h('table', { class: 'diff-table' },
    h('caption', { class: 'visually-hidden' }, `Changes to ${file.path}`),
    h('thead', { class: 'visually-hidden' }, h('tr', null, h('th', { scope: 'col' }, 'Old line'), h('th', { scope: 'col' }, 'New line'), h('th', { scope: 'col' }, 'Change'), h('th', { scope: 'col' }, 'Code'))));
  const body = h('tbody');
  if (!parsed.hunks.length) {
    body.append(h('tr', { class: 'diff-hunk' }, h('td', { colspan: 4 }, file.patch ? 'This change has no line hunks to show.' : 'No diff was recorded for this file.')));
  }
  for (const hk of parsed.hunks) {
    body.append(h('tr', { class: 'diff-hunk' },
      h('td', { colspan: 3, class: 'diff-hunk-gutter' }),
      h('td', null, `Lines ${hk.oldStart} to ${hk.oldStart + Math.max(0, hk.lines.filter((l) => l.type !== 'add' && l.type !== 'meta').length - 1)}`, hk.context ? h('span', { class: 'diff-hunk-fn' }, hk.context) : null)));
    for (const l of hk.lines) {
      if (l.type === 'meta') {
        body.append(h('tr', { class: 'diff-meta' }, h('td', { colspan: 3 }), h('td', null, l.text)));
        continue;
      }
      const sign = l.type === 'add' ? '+' : l.type === 'del' ? '−' : '';
      const said = l.type === 'add' ? 'Added' : l.type === 'del' ? 'Removed' : '';
      body.append(h('tr', { class: `diff-${l.type}` },
        h('td', { class: 'diff-num' }, l.old ?? ''),
        h('td', { class: 'diff-num' }, l.new ?? ''),
        h('td', { class: 'diff-sign' }, h('span', { 'aria-hidden': 'true' }, sign), said ? h('span', { class: 'visually-hidden' }, said) : null),
        h('td', { class: 'diff-code' }, l.text)));
    }
  }
  table.append(body);
  details.append(h('div', { class: 'diff-scroll' }, table));
  return details;
}

export function diffList(files, prefix, state) {
  return h('div', { class: 'diff-list' }, files.map((f) => fileDiff(f, `${prefix}:${f.path}`, state)));
}
