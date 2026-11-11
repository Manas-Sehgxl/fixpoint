// The public status page at /r/{id}, for the person who reported the bug.
// It uses only the public API: no code, no file names, no tool output.

import { h, rich, announce } from './dom.js';
import { api, publicEvents } from './api.js';
import { mark, logo } from './marks.js';
import { compare } from './compare.js';
import { ago, dateTime, clock } from './format.js';

const id = decodeURIComponent((location.pathname.match(/^\/r\/([^/]+)/) || [])[1] || '');
const root = document.getElementById('main');
const conn = document.getElementById('conn');
const STEPS = ['Received', 'Reproduced', 'Fix tested', 'Fixed'];
const SAID = { done: 'done', active: 'in progress', waiting: 'waiting', failed: 'stopped', passed: 'done', pending: 'not yet' };

let report = null;
let cmpPos = 50;
let draft = '';
let sending = false;

document.getElementById('brand').prepend(logo(24));

function stepState(view, i) {
  const n = i + 1;
  if (view.stage === 'fixed') return n === 4 ? 'passed' : 'done';
  if (n < view.step) return 'done';
  if (n > view.step) return 'pending';
  switch (view.stage) {
    case 'question': case 'handed_off': return 'waiting';
    case 'trouble': return 'failed';
    case 'closed': return 'pending';
    default: return 'active';
  }
}

function tracker(view) {
  return h('ol', { class: 'tracker', 'aria-label': 'Progress' }, STEPS.map((label, i) => {
    const st = stepState(view, i);
    return h('li', { class: `is-${st}` }, mark(st), h('span', null, label, h('span', { class: 'visually-hidden' }, `: ${SAID[st]}`)));
  }));
}

function replyBox() {
  const ta = h('textarea', { id: 'answer', rows: 4, 'aria-describedby': 'answer-help' }, draft);
  ta.value = draft;
  ta.addEventListener('input', () => { draft = ta.value; });
  const err = h('p', { class: 'form-error', role: 'alert' });
  const btn = h('button', { type: 'submit', class: 'btn btn-primary', disabled: sending }, sending ? 'Sending' : 'Send your answer');
  const form = h('form', { class: 'reply-box' },
    h('label', { for: 'answer' }, 'Your answer'),
    h('p', { class: 'quiet', id: 'answer-help' }, 'We will try again with what you tell us. This page updates by itself.'),
    ta, err, btn);
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const text = ta.value.trim();
    if (text.length < 2) { err.textContent = 'Write your answer before sending it.'; ta.focus(); return; }
    sending = true;
    btn.disabled = true;
    btn.textContent = 'Sending';
    err.textContent = '';
    try {
      await api.publicReply(id, text);
      draft = '';
      announce('Thank you. Your answer was sent.');
      sending = false;
      load();
    } catch (ex) {
      sending = false;
      err.replaceChildren(rich(ex.message));
      btn.disabled = false;
      btn.textContent = 'Send your answer';
    }
  });
  return form;
}

function render() {
  const r = report;
  const v = r.view;
  document.title = `${v.headline} | Report #${r.seq}`;
  const focused = document.activeElement && document.activeElement.id === 'answer';
  const parts = [
    h('p', { class: 'quiet' }, `Report #${r.seq}`, r.simulated ? h('span', { class: 'simulated-tag' }, 'Simulated run') : ''),
    h('h1', { class: 'status-headline', tabindex: '-1' }, v.headline),
    h('p', { class: 'status-detail' }, v.stage === 'question' ? 'We followed your steps and the app behaved correctly for us. One question will help us see what you saw.' : v.detail),
    tracker(v),
  ];
  if (v.stage === 'question' && v.question) {
    parts.push(h('section', { class: 'status-section', 'aria-labelledby': 'q-h' },
      h('h2', { id: 'q-h' }, 'Our question'),
      h('div', { class: 'words' }, h('p', null, v.question)),
      replyBox()));
  }
  if (v.stage === 'fixed' && v.before_shot && v.after_shot) {
    parts.push(h('section', { class: 'status-section', 'aria-labelledby': 'ba-h' },
      h('h2', { id: 'ba-h' }, 'Before and after the fix'),
      compare({ before: v.before_shot, after: v.after_shot, beforeLabel: 'Before: the problem you reported', afterLabel: 'After: the same steps, fixed', alt: 'Screenshot of the app taken by our automated browser.', start: cmpPos, onMove: (p) => { cmpPos = p; } })));
  }
  parts.push(h('section', { class: 'status-section', 'aria-labelledby': 'you-h' },
    h('h2', { id: 'you-h' }, 'What you told us'),
    h('blockquote', { class: 'words' }, h('p', null, r.text)),
    h('p', { class: 'quiet', style: { marginTop: '10px' } }, `Sent ${dateTime(r.created_at)}.`)));
  const thread = r.thread || [];
  if (thread.length) {
    parts.push(h('section', { class: 'status-section', 'aria-labelledby': 'c-h' },
      h('h2', { id: 'c-h' }, 'Conversation'),
      h('ol', { class: 'thread' }, thread.map((m) => h('li', { class: `msg msg-${m.from === 'reporter' ? 'reporter' : 'fixpoint'}` },
        h('p', { class: 'msg-who' }, m.from === 'reporter' ? 'You' : 'We asked', h('time', { datetime: m.at }, clock(m.at))),
        h('p', { class: 'msg-text' }, m.text))))));
  }
  parts.push(h('p', { class: 'status-foot' }, r.done ? 'This report is closed. ' : 'This page updates by itself. ',
    'Last change ', h('time', { datetime: r.updated_at, dataset: { ago: r.updated_at } }, ago(r.updated_at)), '.'));
  root.replaceChildren(h('div', { class: 'status-page' }, parts));
  if (focused) {
    const ta = document.getElementById('answer');
    if (ta) { ta.focus(); ta.setSelectionRange(ta.value.length, ta.value.length); }
  }
}

function renderError(e) {
  document.title = 'Report not found';
  root.replaceChildren(h('div', { class: 'status-page' },
    h('h1', { class: 'status-headline' }, e.status === 404 ? 'We could not find this report' : 'This page could not load'),
    h('p', { class: 'status-detail' }, e.status === 404 ? 'Check that the address matches the link you were given. Reports from a demo may have been cleared.' : 'The service that tracks your report is not answering right now. Try again in a minute.'),
    e.status === 404 ? null : h('p', { class: 'inline-actions' }, Object.assign(h('button', { type: 'button', class: 'btn' }, 'Try again'), { onclick: () => load() }))));
}

async function load() {
  try {
    report = await api.publicReport(id);
    render();
  } catch (e) {
    if (!report) renderError(e);
  }
}

if (!id) renderError({ status: 404 });
else {
  const slow = setTimeout(() => { if (!report) root.replaceChildren(h('p', { class: 'loading' }, 'Loading your report.')); }, 300);
  load().finally(() => clearTimeout(slow));
  publicEvents(id, {
    onUpdate: (r) => { report = r; render(); },
    onState: (s) => {
      conn.hidden = s !== 'down';
      if (s === 'down') conn.replaceChildren(mark('waiting'), h('span', null, 'Reconnecting. This page will catch up by itself.'));
    },
    onResync: load,
  });
  setInterval(() => { for (const el of document.querySelectorAll('[data-ago]')) el.textContent = ago(el.dataset.ago); }, 30000);
}
