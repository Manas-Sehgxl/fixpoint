// The console shell: one store, one event stream, history routing, and the
// top bar. Views read the store and are told when it changes; on any event
// for a report the report is re-read (debounced), never rebuilt from events.

import { h, announce, clear, rich } from './dom.js';
import { api, events } from './api.js';
import { mark, logo } from './marks.js';
import { duration, since, ago } from './format.js';
import * as board from './views/board.js';
import * as casefile from './views/casefile.js';
import * as suite from './views/suite.js';
import * as setup from './views/setup.js';

const store = {
  system: null, systemError: null,
  list: null, listError: null,
  details: new Map(), detailErrors: new Map(),
  suite: null, suiteError: null,
  live: new Map(), // run id -> Map(step n -> step), from run.step events
  liveSuite: new Map(), // suite test id -> status, while the suite runs
};

let view = null;

function changed() {
  renderChrome();
  if (view) view.update();
}

function debounce(fn, ms) {
  const timers = new Map();
  return (key = '') => {
    clearTimeout(timers.get(key));
    timers.set(key, setTimeout(() => { timers.delete(key); fn(key); }, ms));
  };
}

const inflight = new Map();
function once(key, fn) {
  if (inflight.has(key)) { inflight.get(key).again = true; return; }
  const job = { again: false };
  inflight.set(key, job);
  fn().finally(() => {
    inflight.delete(key);
    if (job.again) once(key, fn);
  });
}

const refresh = {
  system: () => once('system', async () => {
    try { store.system = await api.system(); store.systemError = null; } catch (e) { store.systemError = e; }
    changed();
  }),
  list: () => once('list', async () => {
    try { store.list = await api.reports(); store.listError = null; } catch (e) { store.listError = e; }
    changed();
  }),
  report: (id) => once(`report:${id}`, async () => {
    try {
      const d = await api.report(id);
      store.details.set(id, d);
      store.detailErrors.delete(id);
      for (const run of d.report.runs) if (run.status !== 'running') store.live.delete(run.id);
    } catch (e) {
      store.detailErrors.set(id, e);
      if (e.status === 404) store.details.delete(id);
    }
    changed();
  }),
  suite: () => once('suite', async () => {
    try {
      store.suite = await api.suite();
      store.suiteError = null;
      if (!store.suite.running) store.liveSuite.clear();
    } catch (e) { store.suiteError = e; }
    changed();
  }),
  all() {
    refresh.system();
    refresh.list();
    refresh.suite();
    for (const id of store.details.keys()) refresh.report(id);
  },
};

const later = {
  report: debounce((id) => refresh.report(id), 200),
  list: debounce(() => refresh.list(), 350),
  system: debounce(() => refresh.system(), 500),
  suite: debounce(() => refresh.suite(), 300),
};

function onEvent(ev) {
  if (ev.type === 'run.step') {
    const runId = ev.data && ev.data.run_id;
    if (runId) {
      if (!store.live.has(runId)) store.live.set(runId, new Map());
      store.live.get(runId).set(ev.data.n, { n: ev.data.n, status: ev.data.status, remark: ev.data.remark });
      if (view && view.onStep) view.onStep(ev);
    }
    return;
  }
  if (ev.type.startsWith('suite.')) {
    if (ev.type === 'suite.started') store.liveSuite.clear();
    if (ev.type === 'suite.test' && ev.data) store.liveSuite.set(ev.data.test_id, ev.data.status);
    if (store.suite && ev.type === 'suite.started') store.suite.running = true;
    changed();
    later.suite();
    return;
  }
  if (ev.type === 'system.changed') {
    store.details.clear();
    refresh.all();
    return;
  }
  if (ev.report_id) {
    if (store.details.has(ev.report_id) || (view && view.wants(ev.report_id))) later.report(ev.report_id);
    later.list();
    if (ev.type !== 'run.started' && ev.type !== 'run.finished') later.system();
    if (ev.type === 'report.proven') later.suite();
  }
}

/* Chrome: the top bar state, the simulated notice, the reconnecting note. */

const nav = document.getElementById('nav');
const sysState = document.getElementById('sys-state');
const notices = document.getElementById('notices');
const conn = document.getElementById('conn');
let chromeSig = '';

function renderChrome() {
  const s = store.system;
  const sig = JSON.stringify(s ? [s.simulated, s.kane.problem, s.agent.problem, s.target.reachable, s.target.name] : [store.systemError && store.systemError.message]);
  if (sig === chromeSig) return;
  chromeSig = sig;
  clear(sysState);
  clear(notices);
  if (!s) {
    if (store.systemError) sysState.append(h('a', { href: '/setup', class: 'sys-link is-bad' }, mark('failed'), h('span', null, 'Not connected')));
    return;
  }
  const bad = [!s.target.reachable && `${s.target.name || 'The app'} is not answering`, s.kane.problem && `${s.kane.name} needs attention`, s.agent.problem && `${s.agent.name} needs attention`].filter(Boolean);
  const long = bad.length ? bad[0] : s.simulated ? 'Simulated runners' : `${s.target.name}, ${s.kane.name} and ${s.agent.name} are ready`;
  const short = bad.length ? 'Needs attention' : s.simulated ? 'Simulated' : 'Ready';
  sysState.append(h('a', { href: '/setup', class: bad.length ? 'sys-link is-bad' : 'sys-link' },
    mark(bad.length ? 'failed' : s.simulated ? 'waiting' : 'done'),
    h('span', { class: 'sys-long' }, long),
    h('span', { class: 'sys-short' }, short)));
  if (s.simulated) {
    notices.append(h('div', { class: 'simulated', role: 'note' }, h('div', { class: 'wrap' }, mark('waiting'),
      h('p', null, h('strong', null, 'Simulated runners are on. '), 'No browser is being driven and nothing here is real verification. For development only.'))));
  }
}

function setConn(state) {
  if (state === 'down') {
    conn.hidden = false;
    conn.replaceChildren(mark('waiting'), h('span', null, 'Reconnecting to Fixpoint. Live updates are paused; what you see may be out of date.'));
  } else {
    conn.hidden = true;
    announce('Reconnected. Live updates are back.');
  }
}

/* Actions: a button that runs a request, says what happened, and refreshes. */

function act(btn, fn, okText, after, out) {
  const msgEl = out || (btn.closest('.actions, .stage-inner, section') || document).querySelector('.action-msg');
  const label = btn.textContent;
  btn.disabled = true;
  btn.setAttribute('aria-busy', 'true');
  return fn().then(() => {
    if (msgEl) { msgEl.replaceChildren(okText); msgEl.dataset.keep = '1'; setTimeout(() => { delete msgEl.dataset.keep; }, 4000); }
    announce(okText);
    if (after) after();
  }, (e) => {
    if (msgEl) msgEl.replaceChildren(h('span', { class: 'form-error' }, rich(e.message)));
    else announce(e.message);
  }).finally(() => {
    btn.disabled = false;
    btn.removeAttribute('aria-busy');
    btn.textContent = label;
  });
}

/* Routing. */

const ROUTES = [
  { re: /^\/$/, view: board, nav: '/' },
  { re: /^\/reports\/([^/]+)$/, view: casefile, nav: '/', params: (m) => ({ id: decodeURIComponent(m[1]) }) },
  { re: /^\/suite$/, view: suite, nav: '/suite' },
  { re: /^\/setup$/, view: setup, nav: '/setup' },
];

const main = document.getElementById('main');
const ctx = { store, refresh, api, act, navigate };

function notFound(root) {
  const heading = h('h1', { tabindex: '-1', class: 'msg-title' }, 'There is nothing at this address');
  root.append(heading, h('p', null, 'The console has the reports board, the suite and setup. '), h('p', null, h('a', { class: 'btn', href: '/' }, 'Go to the reports')));
  return { heading: () => heading, title: 'Not found | Fixpoint', update() {}, wants: () => false, unmount() {} };
}

function route(first) {
  const path = location.pathname.replace(/\/+$/, '') || '/';
  let match = null, r = null;
  for (const x of ROUTES) { match = path.match(x.re); if (match) { r = x; break; } }
  if (view) view.unmount();
  clear(main);
  const root = h('div', { class: 'view wrap' });
  main.append(root);
  view = r ? r.view.mount(root, ctx, r.params ? r.params(match) : {}) : notFound(root);
  document.title = view.title;
  for (const a of nav.querySelectorAll('a')) {
    if (r && a.getAttribute('href') === r.nav) a.setAttribute('aria-current', 'page'); else a.removeAttribute('aria-current');
  }
  view.update();
  if (!first) {
    window.scrollTo(0, 0);
    const hd = view.heading();
    (hd || main).focus({ preventScroll: true });
  }
}

function navigate(href) {
  if (href === location.pathname + location.search) return;
  history.pushState({}, '', href);
  route(false);
}

document.addEventListener('click', (e) => {
  if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  const a = e.target.closest('a[href]');
  if (!a || a.target || a.hasAttribute('download')) return;
  const url = new URL(a.href, location.href);
  if (url.origin !== location.origin) return;
  if (/^\/(api|artifacts|r)\//.test(url.pathname) || /\.[a-z0-9]+$/i.test(url.pathname)) return;
  if (url.pathname === location.pathname && url.hash) return;
  e.preventDefault();
  navigate(url.pathname + url.search);
});

window.addEventListener('popstate', () => route(false));

/* Ticking clocks: elapsed times every second, ages every 30 seconds. */

setInterval(() => {
  for (const el of document.querySelectorAll('[data-elapsed-from]')) {
    const t = duration(since(el.dataset.elapsedFrom));
    if (el.textContent !== t) el.textContent = t;
  }
}, 1000);
setInterval(() => {
  for (const el of document.querySelectorAll('[data-ago]')) el.textContent = ago(el.dataset.ago);
}, 30000);

/* Start. */

document.getElementById('brand').prepend(logo(24));
route(true);
refresh.system();
refresh.list();
events({ onEvent, onState: setConn, onResync: () => refresh.all() });
