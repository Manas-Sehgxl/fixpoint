// Pieces shared by several views: Kane's step lists, run blocks, the
// conversation, and section scaffolding for the case file timeline.

import { h, reconcile, rich } from '../dom.js';
import { mark } from '../marks.js';
import { duration, credits, clock, humanize } from '../format.js';

/** mergeSteps combines a run's stored steps with steps seen live on the stream. */
export function mergeSteps(run, live) {
  const map = new Map();
  for (const st of run.steps || []) map.set(st.n, st);
  if (run.status === 'running' && live) for (const st of live.values()) map.set(st.n, st);
  return [...map.values()].sort((a, b) => a.n - b.n);
}

const STEP_MARK = { done: 'done', failed: 'failed', running: 'active' };
const STEP_WORD = { done: 'done', failed: 'failed here', running: 'running now' };

export function stepItem(st) {
  return h('li', { class: `step is-${st.status}` },
    mark(STEP_MARK[st.status] || 'pending'),
    h('span', { class: 'step-n' }, String(st.n)),
    h('span', { class: 'step-remark' }, st.remark || '',
      st.status === 'failed' ? h('span', { class: 'step-flag' }, 'Failed here') : h('span', { class: 'visually-hidden' }, `, ${STEP_WORD[st.status] || st.status}`)));
}

/** stepList returns an <ol> that fillSteps keeps in sync without rebuilding it. */
export function stepList(run, { compact = false } = {}) {
  return h('ol', { class: compact ? 'steps steps-compact' : 'steps', dataset: { run: run.id }, 'aria-label': `Kane's steps for ${run.title || 'this run'}` });
}

export function fillSteps(ol, steps) {
  reconcile(ol, steps, { key: (st) => st.n, sig: (st) => `${st.status}|${st.remark}`, render: stepItem });
}

function runHeadline(run) {
  const kind = run.kind;
  if (run.status === 'running') return kind === 'reproduce' ? 'Kane is running the check in a real browser.' : 'Kane is running the check again.';
  if (run.status === 'error') return 'Kane CLI could not run the check.';
  if (run.status === 'timeout') return 'Kane ran out of time before the check finished.';
  if (kind === 'reproduce') return run.status === 'failed' ? 'Kane reproduced it.' : 'Kane could not reproduce it.';
  if (kind === 'verify') return run.status === 'passed' ? 'The check passes now.' : 'The check still fails.';
  return run.status === 'passed' ? 'Still holds.' : 'This earlier fix broke.';
}

function failedStep(reason) {
  const m = String(reason || '').match(/step\s+(\d+)/);
  return m ? Number(m[1]) : 0;
}

/** runBlock renders one Kane run: headline, steps, values, evidence, verdict. */
export function runBlock(run, ctx, { heading = null } = {}) {
  const el = h('div', { class: `run run-${run.status}` });
  if (heading) el.append(h('h3', { class: 'run-title' }, heading));
  const head = h('p', { class: 'run-headline' }, mark(run.status === 'passed' ? 'passed' : run.status === 'running' ? 'active' : run.status === 'failed' || run.status === 'error' || run.status === 'timeout' ? 'failed' : 'pending'),
    h('span', null, runHeadline(run), run.summary && run.status !== 'running' ? ' ' : '', run.status !== 'running' ? run.summary : ''));
  el.append(head);
  if (run.simulated) el.append(h('p', { class: 'simulated-tag' }, 'Simulated run: the stub runner produced this, not a real browser.'));
  if (run.error) el.append(h('p', { class: 'run-problem' }, rich(run.error)));

  const ol = stepList(run);
  el.append(ol);
  fillSteps(ol, mergeSteps(run, ctx.liveSteps(run.id)));

  if (run.status !== 'running') {
    const facts = [];
    const fs = failedStep(run.reason);
    if (run.status === 'failed' && fs) facts.push(['Failed at', `step ${fs}`]);
    else if (run.reason) facts.push(['Reason', humanize(run.reason)]);
    const values = Object.entries(run.final_state || {});
    for (const [k, v] of values) facts.push([k === 'observed' ? 'Value seen' : humanize(k), typeof v === 'string' ? v : JSON.stringify(v)]);
    if (run.duration_s) facts.push(['Took', duration(run.duration_s)]);
    if (run.credits) facts.push(['Credits', credits(run.credits)]);
    if (run.cli_version) facts.push(['Kane CLI', run.cli_version]);
    if (facts.length) el.append(h('dl', { class: 'facts facts-inline' }, facts.map(([k, v]) => h('div', null, h('dt', null, k), h('dd', null, v)))));
  }

  if (run.bug_verdict && run.bug_verdict.confirmed) {
    const v = run.bug_verdict;
    el.append(h('p', { class: 'verdict' },
      h('strong', null, 'Kane’s verdict. '),
      `Confirmed product bug${v.category ? `, ${humanize(v.category).toLowerCase()}` : ''}${v.severity ? `, ${v.severity} severity` : ''}${v.confidence ? `, confidence ${v.confidence}` : ''}. `,
      v.one_liner || ''));
  }

  const shots = (run.evidence && run.evidence.shots) || [];
  if (shots.length) el.append(evidence(run, shots, ctx));

  const extra = [];
  if (run.evidence && run.evidence.failure_text) {
    extra.push(h('details', { class: 'more' }, h('summary', null, 'Failure record from the evidence pack'), h('pre', { class: 'code' }, run.evidence.failure_text)));
  }
  if (run.evidence && run.evidence.console_errors && run.evidence.console_errors.length) {
    extra.push(h('details', { class: 'more' }, h('summary', null, `Console errors during the run (${run.evidence.console_errors.length})`),
      h('pre', { class: 'code' }, run.evidence.console_errors.join('\n'))));
  }
  if (extra.length) el.append(h('div', { class: 'run-more' }, extra));
  if (run.test_url && /^https?:\/\//.test(run.test_url)) {
    el.append(h('p', { class: 'run-link' }, h('a', { href: run.test_url, target: '_blank', rel: 'noopener noreferrer' }, 'Open this run in Test Manager')));
  }
  return el;
}

function evidence(run, shots, ctx) {
  const key = `shot:${run.id}`;
  const failed = shots.find((x) => x.failed);
  let current = ctx.ui.get(key) ?? (failed || shots[shots.length - 1]).step;
  let annotated = ctx.ui.get(`${key}:ann`) ?? true;
  const img = h('img', { class: 'shot-img', width: 960, height: 600 });
  const cap = h('figcaption', { class: 'shot-cap' });
  const toggle = h('button', { type: 'button', class: 'link-btn' });
  const thumbs = h('div', { class: 'thumbs', role: 'group', 'aria-label': 'Screenshots by step' });
  const show = () => {
    const shot = shots.find((x) => x.step === current) || shots[shots.length - 1];
    const useAnn = annotated && shot.annotated_url;
    img.src = useAnn ? shot.annotated_url : shot.url;
    img.alt = `Kane's screenshot of the app at step ${shot.step}${shot.failed ? ', where the check failed' : ''}${useAnn ? ', with the element it acted on marked' : ''}.`;
    cap.replaceChildren(`Step ${shot.step} of ${shots.length}${shot.failed ? ', where the check failed' : ''}.`);
    if (shot.annotated_url) cap.append(' ', toggle);
    toggle.textContent = useAnn ? 'Show it without the marking' : 'Show the element Kane marked';
    for (const b of thumbs.children) b.setAttribute('aria-pressed', String(Number(b.dataset.step) === shot.step));
  };
  toggle.addEventListener('click', () => { annotated = !annotated; ctx.ui.set(`${key}:ann`, annotated); show(); });
  for (const shot of shots) {
    const b = h('button', { type: 'button', class: shot.failed ? 'thumb is-failed' : 'thumb', dataset: { step: shot.step }, 'aria-label': `Step ${shot.step}${shot.failed ? ', failed' : ''}` },
      h('img', { src: shot.url, alt: '', loading: 'lazy', width: 96, height: 60 }), h('span', { 'aria-hidden': 'true' }, String(shot.step)));
    b.addEventListener('click', () => { current = shot.step; ctx.ui.set(key, current); show(); });
    thumbs.append(b);
  }
  const fig = h('figure', { class: 'shot' }, h('div', { class: 'shot-frame' }, img), cap);
  show();
  return h('div', { class: 'evidence' }, fig, shots.length > 1 ? thumbs : null);
}

/** conversation renders thread messages, oldest first. */
export function conversation(msgs, { reporterName = 'The reporter' } = {}) {
  return h('ol', { class: 'thread', 'aria-label': 'Conversation with the reporter' },
    msgs.map((m) => h('li', { class: `msg msg-${m.from === 'reporter' ? 'reporter' : 'fixpoint'}` },
      h('p', { class: 'msg-who' }, m.from === 'reporter' ? reporterName : 'Fixpoint asked', h('time', { datetime: m.at }, clock(m.at))),
      h('p', { class: 'msg-text' }, m.text))));
}

/**
 * section is one stage in the case file timeline: a rail mark, a heading
 * that opens and closes the stage, and its body.
 */
export function section({ key, title, state, at, offset, body, ui }) {
  const id = `s-${key.replace(/[^\w-]/g, '-')}`;
  const collapsed = ui.get(`collapsed:${key}`) === true;
  const btn = h('button', { type: 'button', class: 'stage-toggle', 'aria-expanded': String(!collapsed), 'aria-controls': `${id}-body` },
    h('span', { class: 'stage-title' }, title), h('span', { class: 'stage-chevron', 'aria-hidden': 'true' }));
  const inner = h('div', { class: 'stage-inner' }, body);
  const wrap = h('div', { class: 'stage-body', id: `${id}-body` }, inner);
  if (collapsed) inner.inert = true;
  const el = h('section', { class: ['stage', `is-${state}`, collapsed && 'is-collapsed'], 'aria-labelledby': `${id}-h` },
    h('span', { class: 'stage-mark' }, mark(state, '')),
    h('header', { class: 'stage-head' },
      h('h2', { id: `${id}-h` }, btn),
      at ? h('p', { class: 'stage-time' }, h('time', { datetime: at }, clock(at)), offset ? h('span', { class: 'stage-offset' }, offset) : null) : null),
    wrap);
  el.querySelector('.stage-mark svg').setAttribute('aria-label', { done: 'Done', passed: 'Passed', failed: 'Failed', active: 'In progress', waiting: 'Waiting for a person', pending: 'Not started' }[state] || state);
  btn.addEventListener('click', () => {
    const now = !el.classList.contains('is-collapsed');
    el.classList.toggle('is-collapsed', now);
    btn.setAttribute('aria-expanded', String(!now));
    inner.inert = now;
    ui.set(`collapsed:${key}`, now);
  });
  return el;
}

