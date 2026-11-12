// Setup: the state of Kane CLI, the coding agent and the target, with
// remedies; the widget snippet; the settings in effect; and, in demo mode,
// resetting the demo behind a confirmation step.

import { h, rich, copyButton, announce } from '../dom.js';
import { mark } from '../marks.js';
import { toolName } from '../format.js';

function toolBlock(t, role) {
  const ok = t.found && !t.problem;
  const name = toolName(t.name);
  const rows = [
    ['Found', t.found ? (t.path ? h('code', null, t.path) : 'Yes') : 'No'],
    ['Version', t.version || 'Unknown'],
  ];
  if (t.signed_in && t.signed_in !== 'unknown') rows.push(['Signed in', t.signed_in === 'yes' ? 'Yes' : 'No']);
  return h('section', { class: `tool ${ok ? 'is-ok' : 'is-bad'}`, 'aria-labelledby': `tool-${role}` },
    h('h2', { id: `tool-${role}` }, mark(ok ? (t.stub ? 'waiting' : 'done') : 'failed', ok ? (t.stub ? 'Simulated' : 'Ready') : 'Needs attention'), ' ', name),
    h('p', { class: 'tool-state' }, ok
      ? (t.stub
        ? `Simulated. A stub runner stands in for ${name}, so nothing here is real verification. Leave --stubs off to use the real one.`
        : `Ready. ${role === 'kane' ? 'Kane drives a real browser for every check.' : 'It writes the checks and the patches.'}`)
      : t.problem),
    !ok && t.remedy ? h('p', { class: 'remedy' }, rich(t.remedy)) : null,
    h('dl', { class: 'facts facts-inline' }, rows.map(([k, v]) => h('div', null, h('dt', null, k), h('dd', null, v)))));
}

export function mount(root, ctx) {
  const heading = h('h1', { tabindex: '-1' }, 'Setup');
  const body = h('div', { class: 'setup' });
  const msg = h('div', { class: 'view-msg' });
  root.append(h('header', { class: 'page-head' }, heading, h('p', { class: 'lede' }, 'What Fixpoint needs, what it found, and how to add it to your own app.')), msg, body);
  let sig = '';
  let loadingTimer = setTimeout(() => { if (!ctx.store.system) msg.replaceChildren(h('p', { class: 'loading' }, 'Checking the tools.')); }, 250);

  function resetBlock() {
    const sec = h('section', { class: 'reset', 'aria-labelledby': 'reset-h' }, h('h2', { id: 'reset-h' }, 'Reset the demo'));
    const intro = h('p', null, 'Start the demo again from nothing: every report, the whole suite and all screenshots are removed, and the demo shop gets its planted bugs back.');
    const start = h('button', { type: 'button', class: 'btn btn-danger-quiet' }, 'Reset the demo');
    const confirm = h('div', { class: 'confirm', hidden: true, role: 'group', 'aria-labelledby': 'confirm-q' },
      h('p', { id: 'confirm-q' }, h('strong', null, 'Remove everything and re-seed the shop? '), 'This cannot be undone.'));
    const yes = h('button', { type: 'button', class: 'btn btn-danger' }, 'Reset everything');
    const no = h('button', { type: 'button', class: 'btn' }, 'Keep everything');
    const out = h('p', { class: 'action-msg', role: 'status' });
    confirm.append(h('div', { class: 'inline-actions' }, yes, no));
    start.addEventListener('click', () => { confirm.hidden = false; start.hidden = true; no.focus(); });
    no.addEventListener('click', () => { confirm.hidden = true; start.hidden = false; start.focus(); });
    yes.addEventListener('click', async () => {
      yes.disabled = true;
      yes.textContent = 'Resetting';
      try {
        await ctx.api.resetDemo();
        out.textContent = 'The demo was reset. The shop has its planted bugs back and the board is empty.';
        announce(out.textContent);
        confirm.hidden = true;
        start.hidden = false;
        ctx.refresh.all();
      } catch (e) {
        out.textContent = '';
        out.append(h('span', { class: 'form-error' }, rich(e.message)));
      } finally {
        yes.disabled = false;
        yes.textContent = 'Reset everything';
      }
    });
    sec.append(intro, start, confirm, out);
    return sec;
  }

  function update() {
    const s = ctx.store.system;
    if (!s) {
      if (ctx.store.systemError) {
        clearTimeout(loadingTimer);
        msg.replaceChildren(h('div', { class: 'problem-block', role: 'alert' }, h('p', null, h('strong', null, 'Fixpoint could not be reached. '), rich(ctx.store.systemError.message)),
          Object.assign(h('button', { type: 'button', class: 'btn' }, 'Try again'), { onclick: () => ctx.refresh.system() })));
      }
      return;
    }
    clearTimeout(loadingTimer);
    msg.replaceChildren();
    const next = JSON.stringify({ ...s, queue: null });
    if (next === sig) return;
    sig = next;
    const t = s.target;
    const settings = [
      ['Fix attempts per report', String(s.max_attempts)],
      ['Autonomy', s.autonomy === 'ask' ? 'Ask: a person approves each fix after Kane reproduces the bug.' : 'Automatic: fixing starts as soon as Kane reproduces the bug.'],
      ['Runners', s.simulated ? 'Simulated. Started with --stubs, for development only.' : 'Real: Kane CLI and Claude Code.'],
      ['Console', s.console_url ? h('code', null, s.console_url) : 'Unknown'],
      ['Fixpoint version', s.version || 'Unknown'],
    ];
    body.replaceChildren(
      h('div', { class: 'tools' },
        toolBlock(s.kane, 'kane'),
        toolBlock(s.agent, 'agent'),
        h('section', { class: `tool ${t.reachable ? 'is-ok' : 'is-bad'}`, 'aria-labelledby': 'tool-target' },
          h('h2', { id: 'tool-target' }, mark(t.reachable ? 'done' : 'failed', t.reachable ? 'Answering' : 'Not answering'), ' ', t.name || 'Target app'),
          h('p', { class: 'tool-state' }, t.reachable ? `Answering at ${t.url}.` : `Not answering at ${t.url}.`, t.demo ? ' This is the built-in demo shop, with five planted bugs.' : ''),
          !t.reachable ? h('p', { class: 'remedy' }, rich(t.demo ? 'Restart `fixpoint up`; it serves the demo shop itself.' : 'Start the app, or correct `target.url` in fixpoint.json.')) : null,
          h('dl', { class: 'facts facts-inline' },
            h('div', null, h('dt', null, 'Address'), h('dd', null, h('a', { href: t.url, target: '_blank', rel: 'noopener noreferrer' }, t.url))),
            t.dir ? h('div', null, h('dt', null, 'Source'), h('dd', null, h('code', null, t.dir))) : null))),
      h('section', { class: 'widget', 'aria-labelledby': 'widget-h' },
        h('h2', { id: 'widget-h' }, 'Add the widget to your app'),
        h('p', null, 'Paste this line into every page, just before the closing body tag. It adds a “Report a problem” button and sends the page address, the visitor’s last actions and any console errors with each report.'),
        h('div', { class: 'snippet' }, h('pre', { class: 'code' }, s.widget_snippet), copyButton('Copy the snippet', () => s.widget_snippet))),
      h('section', { class: 'settings', 'aria-labelledby': 'settings-h' },
        h('h2', { id: 'settings-h' }, 'Settings in effect'),
        h('p', { class: 'quiet' }, 'Change them in fixpoint.json or with flags to ', h('code', null, 'fixpoint up'), ', then restart.'),
        h('dl', { class: 'facts facts-table' }, settings.map(([k, v]) => h('div', null, h('dt', null, k), h('dd', null, v))))),
    );
    if (t.demo) body.append(resetBlock());
  }

  return { heading: () => heading, title: 'Setup | Fixpoint', update, wants: () => false, unmount() { clearTimeout(loadingTimer); } };
}
