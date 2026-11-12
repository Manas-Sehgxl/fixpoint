// The reports board: the system line, the summary as a sentence, the live
// panel for whatever the worker is doing, and the reports as rows.

import { h, reconcile, rich, copyButton } from '../dom.js';
import { mark, track, trackLegend } from '../marks.js';
import { createCobweb } from '../cobweb.js';
import {
  duration, ago, since, statusText, stageSentence, markFor, toolName, ACTIVE, WORKING, NEEDS_PERSON,
} from '../format.js';
import { fillSteps, mergeSteps } from './parts.js';

const FILTERS = [
  { key: 'all', label: 'All', test: () => true },
  { key: 'in-progress', label: 'In progress', test: (r) => ACTIVE.includes(r.status) },
  { key: 'proven', label: 'Proven', test: (r) => r.status === 'proven' },
  { key: 'needs-you', label: 'Needs you', test: (r) => NEEDS_PERSON.includes(r.status) },
];

const EMPTY_FILTER = {
  'in-progress': 'Nothing is being worked on right now.',
  proven: 'No report has been proven yet.',
  'needs-you': 'Nothing needs you right now.',
};

export function summarySentence(stats) {
  const n = stats.reports;
  const parts = [`${n} ${n === 1 ? 'report' : 'reports'}.`];
  if (stats.proven) {
    parts.push(`${stats.proven} proven${stats.median_time_to_proof_s ? `, in a median of ${duration(stats.median_time_to_proof_s)} from report to proof` : ''}.`);
  } else parts.push('None proven yet.');
  if (stats.in_progress) parts.push(`${stats.in_progress} in progress.`);
  if (stats.needs_you) parts.push(`${stats.needs_you} ${stats.needs_you === 1 ? 'needs' : 'need'} you.`);
  return parts.join(' ');
}

/** systemLine says in words what Fixpoint is watching and whether its tools are ready. */
export function systemLine(sys) {
  if (!sys) return null;
  const t = sys.target;
  const problems = [];
  if (!t.reachable) problems.push({ text: `${t.name || 'The target app'} does not answer at ${t.url}.`, remedy: t.demo ? 'Fixpoint serves the demo shop itself: restart `fixpoint up`.' : 'Start the app, or check `target.url` in fixpoint.json.' });
  for (const tool of [sys.kane, sys.agent]) if (tool.problem) problems.push({ text: tool.problem, remedy: tool.remedy });
  const plain = (tool) => toolName(tool.name);
  const ok = (tool) => `${plain(tool)} ${tool.version ? tool.version + ' ' : ''}${tool.signed_in === 'yes' ? 'is signed in' : 'is ready'}`;
  const parts = [];
  if (t.reachable) parts.push(h('span', { class: 'sys-item' }, mark('done'), `Watching ${t.name} at `, h('a', { href: t.url, target: '_blank', rel: 'noopener noreferrer' }, t.url.replace(/^https?:\/\//, '')), '.'));
  for (const tool of [sys.kane, sys.agent]) {
    // A simulated runner is said once, by the banner at the top of every page.
    if (tool.problem || tool.stub) continue;
    parts.push(h('span', { class: 'sys-item' }, mark('done'), `${ok(tool)}.`));
  }
  const line = h('p', { class: 'sysline' }, parts);
  if (!problems.length) return line;
  return h('div', null, line, h('div', { class: 'sys-problems', role: 'alert' }, problems.map((p) => h('p', { class: 'sys-problem' }, mark('failed'),
    h('span', null, h('strong', null, p.text), ' ', p.remedy ? rich(p.remedy) : '', ' ', h('a', { href: '/setup' }, 'Open Setup'))))));
}

function liveRun(r) {
  const runs = r.runs || [];
  return runs.find((x) => x.status === 'running') || runs[runs.length - 1] || null;
}

const RUN_LABEL = { reproduce: 'Kane’s first run', verify: 'Kane’s verify run', regression: 'Re-checking an earlier fix', suite: 'Suite run' };

function liveRunTitle(run, r) {
  if (!run) return '';
  if (run.kind === 'verify') return `Kane’s verify run for attempt ${run.attempt}`;
  if (run.kind === 'regression') return `Re-checking ${run.suite_test_id ? `test ${run.suite_test_id.replace(/^t_/, '')}` : 'an earlier fix'}: ${run.title}`;
  const n = r.runs.filter((x) => x.kind === 'reproduce').indexOf(run);
  return n > 0 ? 'Kane’s run with the reporter’s answer' : RUN_LABEL[run.kind] || run.title;
}

function createLive(ctx) {
  const plot = createCobweb({ compact: true });
  const title = h('h2', { class: 'live-title' });
  const stage = h('p', { class: 'live-stage', 'aria-live': 'polite' });
  const meta = h('dl', { class: 'facts facts-inline live-meta' });
  const runTitle = h('h3', { class: 'live-run' });
  const steps = h('ol', { class: 'steps steps-live', 'aria-label': 'Kane’s steps, live' });
  const feed = h('div', { class: 'live-feed', tabindex: '0', 'aria-label': 'Kane’s steps as they arrive' }, steps);
  const el = h('section', { class: 'live', 'aria-labelledby': 'live-title' },
    h('div', { class: 'live-plot' }, plot.el),
    h('div', { class: 'live-body' }, title, stage, meta, runTitle, feed));
  title.id = 'live-title';
  let stick = true;
  feed.addEventListener('scroll', () => { stick = feed.scrollTop + feed.clientHeight >= feed.scrollHeight - 8; });
  let shownRun = null;

  function update(r) {
    const link = h('a', { href: `/reports/${r.id}` }, h('span', { class: 'case-num' }, `#${r.seq}`), ' ', r.title);
    title.replaceChildren(link);
    stage.textContent = r.proof
      ? `Proven in ${duration(r.proof.time_to_proof_s)}.${r.proof.suite_test_id ? ` Its check joins the suite as test ${r.proof.suite_test_id.replace(/^t_/, '')}.` : ''}`
      : stageSentence(r);
    stage.prepend(mark(markFor(r.status)), ' ');
    const f = [];
    f.push(['Attempt', r.attempts.length ? `${r.attempts.length} of ${r.max_attempts}` : `0 of ${r.max_attempts}`]);
    if (r.proof) f.push(['Time to proof', duration(r.proof.time_to_proof_s)]);
    else if (ACTIVE.includes(r.status)) f.push(['Elapsed', h('span', { class: 'elapsed', dataset: { elapsedFrom: r.created_at } }, duration(since(r.created_at)))]);
    meta.replaceChildren(...f.map(([k, v]) => h('div', null, h('dt', null, k), h('dd', null, v))));
    plot.update(r);
    const run = liveRun(r);
    if (!run) {
      runTitle.textContent = r.status === 'triaging' ? 'Claude Code is writing the check. Kane’s steps appear here when it starts.' : 'Kane’s steps appear here when it starts.';
      steps.replaceChildren();
      shownRun = null;
      return;
    }
    runTitle.textContent = run.status === 'running' ? liveRunTitle(run, r) : `${liveRunTitle(run, r).replace(/^Re-checking/, 'Re-checked')}, finished`;
    if (shownRun !== run.id) { steps.replaceChildren(); shownRun = run.id; stick = true; }
    fillSteps(steps, mergeSteps(run, ctx.store.live.get(run.id)));
    if (stick) feed.scrollTop = feed.scrollHeight;
  }

  function step(ev, r) {
    if (!shownRun || ev.data.run_id !== shownRun) return false;
    const run = r.runs.find((x) => x.id === shownRun) || { id: shownRun, steps: [] };
    fillSteps(steps, mergeSteps({ ...run, status: 'running' }, ctx.store.live.get(shownRun)));
    if (stick) feed.scrollTop = feed.scrollHeight;
    return true;
  }
  return { el, update, step };
}

// What a person does about a report that waits for them, and the case file
// is where they do it.
const ACTION = {
  needs_info: 'Reply',
  awaiting_approval: 'Review the fix',
  escalated: 'Take it over',
  error: 'See what to do',
};

/** sameText says whether a report's description only repeats its title. */
function sameText(a, b) {
  const norm = (t) => String(t || '').trim().replace(/\s+/g, ' ').replace(/[.!?]+$/, '').toLowerCase();
  return norm(a) === norm(b);
}

function row(r) {
  const att = r.attempts ? `${r.attempts} of ${r.max_attempts}` : '';
  const action = NEEDS_PERSON.includes(r.status) && ACTION[r.status];
  const ttp = r.time_to_proof_s
    ? [h('span', { class: 'm-label' }, 'Proven in '), h('span', { class: 'ttp-value', dataset: { seconds: r.time_to_proof_s } }, duration(r.time_to_proof_s))]
    : h('span', { class: 'visually-hidden' }, 'Not proven');
  const provenAt = justProven.get(r.id);
  return h('tr', {
    class: ['row', `is-${r.status}`, action && 'is-needs-you', provenAt !== undefined && 'just-proven'],
    // A row redrawn mid-moment picks the animation up where it was.
    style: provenAt !== undefined ? `--since: ${Math.round(provenAt - performance.now())}ms` : null,
  },
    h('td', { class: 'c-num' }, `#${r.seq}`),
    h('td', { class: 'c-title' },
      h('a', { href: `/reports/${r.id}`, class: 'row-link' }, r.title),
      sameText(r.title, r.text) ? null : h('span', { class: 'row-text' }, r.text)),
    h('td', { class: 'c-status' }, mark(markFor(r.status)), h('span', null, statusText(r)),
      action ? h('a', { class: 'btn btn-primary btn-row', href: `/reports/${r.id}` }, action, h('span', { class: 'visually-hidden' }, ` on report ${r.seq}`)) : null),
    h('td', { class: 'c-track' }, track(r.track, { tips: true })),
    h('td', { class: 'c-att' }, att ? [h('span', { class: 'm-label' }, 'Attempts '), att] : h('span', { class: 'visually-hidden' }, 'None')),
    h('td', { class: 'c-ttp' }, ttp),
    h('td', { class: 'c-age' }, h('span', { class: 'm-label' }, 'Filed '), h('time', { datetime: r.created_at, dataset: { ago: r.created_at } }, ago(r.created_at))));
}

// Reports seen turning proven while this page was open, so their rows can
// mark the moment once: a green pulse, a brief highlight, the time counting up.
const justProven = new Map(); // report id -> when it turned proven
const lastStatus = new Map();
const calm = () => matchMedia('(prefers-reduced-motion: reduce)').matches;

function notePromotions(list) {
  for (const r of list) {
    const was = lastStatus.get(r.id);
    if (was && was !== 'proven' && r.status === 'proven') {
      justProven.set(r.id, performance.now());
      setTimeout(() => justProven.delete(r.id), 2000);
    }
    lastStatus.set(r.id, r.status);
  }
}

/** countUp runs a proven row's time to proof from zero to its value, once, timed from when it turned proven. */
function countUp(el, t0) {
  if (el.dataset.counted) return;
  el.dataset.counted = '1';
  const end = Number(el.dataset.seconds);
  if (calm() || !(end > 0) || t0 === undefined) return;
  const ms = 900;
  const show = (now) => {
    const k = Math.min(1, Math.max(0, (now - t0) / ms));
    el.textContent = duration(Math.round(end * (1 - Math.pow(1 - k, 3))));
    if (k < 1) requestAnimationFrame(show);
  };
  show(performance.now());
}

export function mount(root, ctx) {
  const params = new URLSearchParams(location.search);
  let filter = FILTERS.some((f) => f.key === params.get('show')) ? params.get('show') : 'all';
  const heading = h('h1', { class: 'visually-hidden', tabindex: '-1' }, 'Reports');
  const sys = h('div', { class: 'board-sys' });
  const summary = h('p', { class: 'summary', 'aria-live': 'polite' });
  const liveSlot = h('div', { class: 'live-slot' });
  const filters = h('div', { class: 'filters', role: 'group', 'aria-label': 'Show reports' });
  const tbody = h('tbody');
  const table = h('table', { class: 'rows' },
    h('caption', { class: 'visually-hidden' }, 'Reports, newest first'),
    h('thead', null, h('tr', null,
      h('th', { scope: 'col', class: 'c-num' }, h('span', { class: 'visually-hidden' }, 'Number')),
      h('th', { scope: 'col', class: 'c-title' }, 'Report'),
      h('th', { scope: 'col', class: 'c-status' }, 'Status'),
      h('th', { scope: 'col', class: 'c-track' }, 'Track', trackLegend()),
      h('th', { scope: 'col', class: 'c-att' }, 'Attempts'),
      h('th', { scope: 'col', class: 'c-ttp' }, 'Time to proof'),
      h('th', { scope: 'col', class: 'c-age' }, 'Filed'))),
    tbody);
  const filterEmpty = h('p', { class: 'filter-empty' });
  const listWrap = h('section', { class: 'list', 'aria-label': 'Reports' }, filters, table, filterEmpty);
  const empty = h('div', { class: 'empty' });
  const msg = h('div', { class: 'view-msg' });
  root.append(heading, msg, h('div', { class: 'board-head' }, summary, sys), liveSlot, listWrap, empty);
  let live = null;
  let liveId = null;
  let loadingTimer = setTimeout(() => { if (!ctx.store.list) msg.replaceChildren(h('p', { class: 'loading' }, 'Loading reports.')); }, 250);

  const buttons = FILTERS.map((f) => {
    const b = h('button', { type: 'button', class: 'filter', 'aria-pressed': String(f.key === filter) }, h('span', null, f.label), h('span', { class: 'filter-count' }));
    b.addEventListener('click', () => {
      filter = f.key;
      const u = new URL(location.href);
      if (filter === 'all') u.searchParams.delete('show'); else u.searchParams.set('show', filter);
      history.replaceState(history.state, '', u);
      update();
    });
    filters.append(b);
    return b;
  });

  function pickLive(list) {
    const sysActive = ctx.store.system && ctx.store.system.queue.active_report_id;
    const working = list.find((r) => r.id === sysActive) || list.find((r) => WORKING.includes(r.status));
    if (working) return working.id;
    // Keep the last one on screen after it finishes, so its ending can be seen.
    return liveId && list.some((r) => r.id === liveId) ? liveId : null;
  }

  function update() {
    const L = ctx.store.list;
    sys.replaceChildren(systemLine(ctx.store.system) || '');
    if (!L) {
      if (ctx.store.listError) {
        clearTimeout(loadingTimer);
        msg.replaceChildren(h('div', { class: 'problem-block', role: 'alert' },
          h('p', null, h('strong', null, 'The reports could not be loaded. '), rich(ctx.store.listError.message)),
          Object.assign(h('button', { type: 'button', class: 'btn' }, 'Try again'), { onclick: () => { ctx.refresh.list(); ctx.refresh.system(); } })));
      }
      listWrap.hidden = true;
      return;
    }
    clearTimeout(loadingTimer);
    msg.replaceChildren();
    const list = L.reports;
    if (!list.length) {
      summary.textContent = 'No reports yet.';
      listWrap.hidden = true;
      liveSlot.replaceChildren();
      renderEmpty();
      return;
    }
    empty.replaceChildren();
    listWrap.hidden = false;
    summary.textContent = summarySentence(L.stats);

    const nextLive = pickLive(list);
    if (nextLive !== liveId) {
      liveId = nextLive;
      if (liveId) ctx.refresh.report(liveId);
    }
    const d = liveId && ctx.store.details.get(liveId);
    if (d) {
      if (!live) { live = createLive(ctx); }
      if (!liveSlot.contains(live.el)) liveSlot.replaceChildren(live.el);
      live.update(d.report);
    } else if (!liveId) {
      liveSlot.replaceChildren();
    }

    FILTERS.forEach((f, i) => {
      buttons[i].setAttribute('aria-pressed', String(f.key === filter));
      buttons[i].querySelector('.filter-count').textContent = String(list.filter(f.test).length);
    });
    notePromotions(list);
    const shown = list.filter(FILTERS.find((f) => f.key === filter).test);
    reconcile(tbody, shown, { key: (r) => r.id, sig: (r) => JSON.stringify(r) + justProven.has(r.id), render: row });
    for (const el of tbody.querySelectorAll('.just-proven .ttp-value')) countUp(el, justProven.get(el.closest('tr').dataset.key));
    table.hidden = !shown.length;
    filterEmpty.textContent = shown.length ? '' : EMPTY_FILTER[filter] || '';
  }

  function renderEmpty() {
    if (empty.childElementCount) return;
    const s = ctx.store.system;
    const t = s && s.target;
    const plot = createCobweb();
    plot.update({ id: 'empty', status: 'queued', runs: [], attempts: [] });
    plot.el.querySelector('figcaption').remove();
    const steps = h('div', { class: 'empty-text' },
      h('h2', null, 'Report a problem to see the loop run'),
      t && t.demo
        ? h('p', null, 'Open the demo shop, do something that goes wrong (remove an item and watch the total, or try the coupon in lower case), then press “Report a problem” at the bottom right. The report appears here, and Kane starts on it right away.')
        : h('p', null, `Open ${t ? t.name || t.url : 'your app'}, find something that goes wrong, and press “Report a problem”. The report appears here, and Kane starts on it right away.`),
      t ? h('p', null, h('a', { class: 'btn btn-primary', href: t.url, target: '_blank', rel: 'noopener noreferrer' }, t.demo ? 'Open the demo shop' : `Open ${t.name || 'the app'}`)) : null,
      h('h3', null, 'For your own app'),
      h('p', null, 'Add this line to every page, then set ', h('code', null, 'target.url'), ' and ', h('code', null, 'target.dir'), ' in fixpoint.json (', h('code', null, 'fixpoint init'), ' writes a starter).'),
      s ? h('div', { class: 'snippet' }, h('pre', { class: 'code' }, s.widget_snippet), copyButton('Copy the snippet', () => s.widget_snippet)) : null);
    empty.append(h('div', { class: 'empty-grid' }, steps, h('div', { class: 'empty-plot' }, plot.el)));
  }

  function onStep(ev) {
    if (!live || ev.report_id !== liveId) return;
    const d = ctx.store.details.get(liveId);
    if (d) live.step(ev, d.report);
  }

  return {
    heading: () => heading,
    title: 'Reports | Fixpoint',
    update,
    onStep,
    wants: (reportId) => reportId === liveId,
    unmount() { clearTimeout(loadingTimer); },
  };
}

