// The regression suite: every check that proved a fix, re-run after each
// new patch, and runnable as a whole on demand.

import { h, reconcile, rich } from '../dom.js';
import { mark } from '../marks.js';
import { duration, ago } from '../format.js';

const RESULT = { passed: 'Passed', failed: 'Failed', error: 'Could not run', timeout: 'Timed out', running: 'Running' };

function testRow(t, live) {
  const now = live.get(t.id);
  const lr = t.last_run;
  let state = 'pending', words = 'Not run since it was added';
  if (now === 'running') { state = 'active'; words = 'Running now'; }
  else if (lr) {
    state = lr.status === 'passed' ? 'passed' : lr.status === 'running' ? 'active' : 'failed';
    words = `${RESULT[lr.status] || lr.status} `;
  }
  return h('li', { class: `test is-${state}` },
    h('span', { class: 'test-mark' }, mark(state, words.trim())),
    h('div', { class: 'test-main' },
      h('h2', { class: 'test-title' }, h('span', { class: 'case-num' }, `Test ${t.id.replace(/^t_/, '')}`), ' ', t.title),
      h('p', { class: 'test-from' }, 'Proved the fix for ', h('a', { href: `/reports/${t.report_id}` }, `report #${t.report_seq}`), '.',
        t.test_file ? ' Saved as a replayable Kane test.' : ''),
      h('details', { class: 'more' }, h('summary', null, 'Show the check'), h('pre', { class: 'code check-text' }, t.objective))),
    h('p', { class: 'test-result' },
      h('span', { class: 'test-words' }, now === 'running' ? 'Running now' : lr ? `${RESULT[lr.status] || lr.status} ` : words,
        !now && lr ? h('time', { datetime: lr.at, dataset: { ago: lr.at } }, ago(lr.at)) : null),
      !now && lr ? h('span', { class: 'quiet' }, `${duration(lr.duration_s)}, ${lr.replayed ? 'replayed from its recording' : 'run fresh by Kane'}`) : null,
      !now && lr && lr.reason && lr.status !== 'passed' ? h('span', { class: 'test-reason' }, lr.reason) : null));
}

export function mount(root, ctx) {
  const heading = h('h1', { tabindex: '-1' }, 'Suite');
  const lede = h('p', { class: 'lede' }, 'Every proven fix leaves its check behind, and all of them re-run after each new patch, so a fix that breaks an earlier one never counts.');
  const btn = h('button', { type: 'button', class: 'btn btn-primary' }, 'Run all checks');
  const progress = h('p', { class: 'suite-progress', role: 'status' });
  const list = h('ol', { class: 'tests', 'aria-label': 'Suite tests' });
  const empty = h('div', { class: 'empty-short' });
  const msg = h('div', { class: 'view-msg' });
  root.append(h('header', { class: 'page-head' }, heading, lede, h('div', { class: 'inline-actions' }, btn, progress)), msg, list, empty);
  let loadingTimer = setTimeout(() => { if (!ctx.store.suite) msg.replaceChildren(h('p', { class: 'loading' }, 'Loading the suite.')); }, 250);
  ctx.refresh.suite();

  btn.addEventListener('click', () => ctx.act(btn, () => ctx.api.runSuite(), 'Running every check. Results appear as they finish.', () => ctx.refresh.suite(), progress));

  function update() {
    const S = ctx.store.suite;
    if (!S) {
      if (ctx.store.suiteError) {
        clearTimeout(loadingTimer);
        msg.replaceChildren(h('div', { class: 'problem-block', role: 'alert' }, h('p', null, h('strong', null, 'The suite could not be loaded. '), rich(ctx.store.suiteError.message)),
          Object.assign(h('button', { type: 'button', class: 'btn' }, 'Try again'), { onclick: () => ctx.refresh.suite() })));
      }
      btn.hidden = true;
      return;
    }
    clearTimeout(loadingTimer);
    msg.replaceChildren();
    const live = ctx.store.liveSuite;
    btn.hidden = !S.tests.length;
    btn.disabled = S.running;
    btn.textContent = S.running ? 'Running all checks' : 'Run all checks';
    if (S.running) {
      const done = S.tests.filter((t) => live.get(t.id) && live.get(t.id) !== 'running').length;
      const cur = S.tests.find((t) => live.get(t.id) === 'running');
      progress.textContent = `${done} of ${S.tests.length} done.${cur ? ` Now running test ${cur.id.replace(/^t_/, '')}: ${cur.title}.` : ''}`;
    } else if (!progress.dataset.keep) progress.textContent = '';
    if (!S.tests.length) {
      list.replaceChildren();
      if (!empty.childElementCount) {
        empty.append(h('p', null, 'The suite is empty. When Fixpoint proves a fix, the check that proved it lands here and re-runs after every later patch.'),
          h('p', null, h('a', { href: '/' }, 'Go to the reports')));
      }
      return;
    }
    empty.replaceChildren();
    reconcile(list, S.tests, { key: (t) => t.id, sig: (t) => JSON.stringify([t, live.get(t.id) || '']), render: (t) => testRow(t, live) });
  }

  return { heading: () => heading, title: 'Suite | Fixpoint', update, wants: () => false, unmount() { clearTimeout(loadingTimer); } };
}
