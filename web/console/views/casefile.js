// The case file: the whole loop for one report, live. The timeline is built
// from the report itself on every re-read; sections are keyed so only the
// ones that changed are rebuilt, and steps arrive straight from the stream.

import { h, reconcile, rich, copyButton, announce } from '../dom.js';
import { mark } from '../marks.js';
import { createCobweb } from '../cobweb.js';
import { diffList } from '../diff.js';
import { compare } from '../compare.js';
import { api } from '../api.js';
import {
  duration, credits, usd, clock, dateTime, plural, countWord, statusText, markFor, browser, since, ordinal, ACTIVE,
} from '../format.js';
import { runBlock, conversation, section, fillSteps, mergeSteps } from './parts.js';

const RUN_ORDINAL = ['first', 'second', 'third', 'fourth', 'fifth', 'sixth'];

const ATTEMPT_LINE = {
  running: 'In progress', verified: 'Held', still_failing: 'Did not hold', regressed: 'Broke an earlier fix',
  no_change: 'Changed nothing', revised_check: 'Corrected the check instead of the app', gave_up: 'Gave up', agent_error: 'The agent could not run',
};
const ATTEMPT_MARK = { running: 'active', verified: 'passed', still_failing: 'failed', regressed: 'failed', no_change: 'failed', revised_check: 'done', gave_up: 'failed', agent_error: 'failed' };

function runState(run) {
  if (run.status === 'running') return 'active';
  if (run.status === 'passed') return run.kind === 'reproduce' ? 'done' : 'passed';
  return run.kind === 'reproduce' && run.status === 'failed' ? 'done' : 'failed';
}

function offsetFrom(r, at) {
  const s = (Date.parse(at) - Date.parse(r.created_at)) / 1000;
  return s >= 1 ? `+${duration(s)}` : '';
}

/** plan lists the timeline's sections in the order things happened. */
function plan(r, events) {
  const out = [];
  const firstAt = (type, pred) => (events.find((e) => e.type === type && (!pred || pred(e))) || {}).at;
  out.push({ key: 'reported', kind: 'reported', at: r.created_at, state: 'done', title: 'Reported' });

  if (r.check) {
    const at = [...events].reverse().find((e) => e.type === 'check.written')?.at || r.created_at;
    out.push({ key: `check:${r.check.revision}`, kind: 'check', at, state: 'done', title: r.check.revision > 1 ? 'Check corrected' : 'Check written' });
  } else if (r.status === 'triaging') {
    out.push({ key: 'check-pending', kind: 'triaging', at: firstAt('report.status', (e) => e.data && e.data.status === 'triaging') || r.updated_at, state: 'active', title: 'Writing the check' });
  }

  const repro = r.runs.filter((x) => x.kind === 'reproduce');
  const thread = [...(r.thread || [])];
  const early = thread.filter((m) => !repro.length || Date.parse(m.at) < Date.parse(repro[0].started_at));
  if (early.length) out.push({ key: 'ask-early', kind: 'early-thread', at: early[0].at, state: r.status === 'needs_info' && early.length === thread.length ? 'waiting' : 'done', title: 'Question for the reporter', msgs: early });
  repro.forEach((run, i) => {
    const next = repro[i + 1];
    const msgs = thread.filter((m) => Date.parse(m.at) >= Date.parse(run.started_at) && (!next || Date.parse(m.at) < Date.parse(next.started_at)));
    const lastRun = i === repro.length - 1;
    let state = runState(run);
    if (lastRun && run.status === 'passed' && r.status === 'needs_info') state = 'waiting';
    out.push({
      key: `run:${run.id}`, kind: 'reproduce', run, msgs, lastRun, at: run.started_at, state,
      title: `Kane’s ${RUN_ORDINAL[i] || ordinal(i + 1)} run`,
    });
  });

  for (const a of r.attempts) {
    const verify = r.runs.find((x) => x.id === a.verify_run_id) || r.runs.find((x) => x.kind === 'verify' && x.attempt === a.n);
    let state = ATTEMPT_MARK[a.status] || 'done';
    if (a.status === 'running' && verify && verify.status === 'passed') state = 'active';
    out.push({ key: `attempt:${a.n}:${a.started_at}`, kind: 'attempt', a, verify, at: a.started_at, state, title: `Attempt ${a.n} of ${r.max_attempts}` });
    const regs = r.runs.filter((x) => x.kind === 'regression' && x.attempt === a.n);
    if (regs.length) {
      const anyRun = regs.some((x) => x.status === 'running');
      const anyFail = regs.some((x) => x.status !== 'passed' && x.status !== 'running');
      out.push({ key: `regress:${a.n}:${a.started_at}`, kind: 'regress', runs: regs, a, at: regs[0].started_at, state: anyRun ? 'active' : anyFail ? 'failed' : 'passed', title: 'Earlier fixes re-checked' });
    }
  }

  switch (r.status) {
    case 'proven': out.push({ key: 'proof', kind: 'proof', at: r.closed_at || r.updated_at, state: 'passed', title: 'Proven' }); break;
    case 'escalated': out.push({ key: 'escalated', kind: 'escalated', at: r.closed_at || r.updated_at, state: 'failed', title: 'Handed to a person' }); break;
    case 'error': out.push({ key: `error:${r.updated_at}`, kind: 'error', at: r.updated_at, state: 'failed', title: 'Stopped by an error' }); break;
    case 'unreproduced': out.push({ key: 'closed', kind: 'unreproduced', at: r.closed_at || r.updated_at, state: 'done', title: 'Closed without a reproduction' }); break;
    case 'awaiting_approval': out.push({ key: 'approval', kind: 'approval', at: r.updated_at, state: 'waiting', title: 'Waiting for your approval' }); break;
    case 'queued': out.push({ key: `queued:${r.updated_at}`, kind: 'queued', at: r.updated_at, state: 'pending', title: 'In line' }); break;
    default: break;
  }
  return out.map((x, i) => ({ ...x, i })).sort((a, b) => (Date.parse(a.at) - Date.parse(b.at)) || a.i - b.i);
}

function stripSteps(run) {
  if (!run) return run;
  const { steps, ...rest } = run;
  return { ...rest, nSteps: run.status === 'running' ? 0 : (steps || []).length };
}

function sigOf(item, r) {
  const x = { ...item, run: stripSteps(item.run), verify: stripSteps(item.verify), runs: item.runs && item.runs.map(stripSteps) };
  return JSON.stringify([x, item.kind === 'reproduce' || item.kind === 'approval' || item.kind === 'queued' ? [r.status, r.queue_position] : 0]);
}

function lead(label, text) {
  return h('p', { class: 'lead-in' }, h('strong', null, `${label}. `), text);
}

function body(item, r, ctx) {
  switch (item.kind) {
    case 'reported': return reportedBody(r);
    case 'check': return checkBody(r);
    case 'triaging': return h('p', { class: 'working' }, 'Claude Code is reading the report and the app’s source to write one browser check that fails while the bug exists.');
    case 'early-thread': return h('div', null, conversation(item.msgs, { reporterName: reporterName(r) }));
    case 'reproduce': return reproduceBody(item, r, ctx);
    case 'attempt': return attemptBody(item, r, ctx);
    case 'regress': return regressBody(item, r, ctx);
    case 'proof': return proofBody(r, ctx);
    case 'escalated': return escalatedBody(r);
    case 'error': return errorBody(r);
    case 'unreproduced': return h('p', null, `Kane ran the check ${plural(r.runs.filter((x) => x.kind === 'reproduce').length, 'time')} and the app behaved correctly every time, so the report was closed. Nothing was changed in the app.`);
    case 'approval': return approvalBody(r, ctx);
    case 'queued': return h('p', null, r.queue_position > 1 ? `${ordinal(r.queue_position)} in line. Fixpoint works on one report at a time because fixes change the same files.` : 'Next in line. Kane starts on it as soon as the worker is free.');
    default: return h('p', null, '');
  }
}

function reporterName(r) {
  return r.reporter && (r.reporter.name || r.reporter.email) ? (r.reporter.name || r.reporter.email) : 'The reporter';
}

function safeLink(url) {
  return /^https?:\/\//i.test(url) ? h('a', { href: url, target: '_blank', rel: 'noopener noreferrer' }, url) : url;
}

function reportedBody(r) {
  const c = r.context || {};
  const who = r.reporter && (r.reporter.name || r.reporter.email);
  const meta = [];
  meta.push(who ? `From ${r.reporter.name || ''}${r.reporter.name && r.reporter.email ? ` (${r.reporter.email})` : r.reporter.email && !r.reporter.name ? r.reporter.email : ''}, on ` : 'From someone who left no name, on ');
  meta.push(c.page_url ? safeLink(c.page_url) : 'an unknown page');
  const env = [browser(c.user_agent), c.viewport_w ? `${c.viewport_w} × ${c.viewport_h}` : ''].filter(Boolean).join(' at ');
  meta.push(env ? `, using ${env}.` : '.');
  const crumbs = c.breadcrumbs || [];
  const errs = c.console_errors || [];
  return h('div', { class: 'reported' },
    h('blockquote', { class: 'words' }, h('p', null, r.text)),
    h('p', { class: 'meta' }, meta),
    h('h3', null, 'What they did before reporting'),
    crumbs.length ? h('ol', { class: 'crumbs' }, crumbs.map((b) => h('li', { class: `crumb crumb-${b.kind}` },
      h('span', { class: 'crumb-text' }, b.text), h('span', { class: 'crumb-ago' }, `${duration(b.ago_ms / 1000)} before`))))
      : h('p', { class: 'quiet' }, 'The widget recorded no actions before the report.'),
    h('h3', null, 'Errors in their browser console'),
    errs.length ? h('pre', { class: 'code' }, errs.join('\n')) : h('p', { class: 'quiet' }, 'None were recorded.'));
}

function checkBody(r) {
  const c = r.check;
  return h('div', { class: 'check' },
    h('p', null, `${c.written_by || 'The coding agent'} wrote this check from the report. Kane runs it word for word.`,
      c.revision > 1 ? ' This is revision 2: the agent found its first check was wrong and corrected it.' : ''),
    h('pre', { class: 'code check-text' }, c.objective),
    lead('Passes when', c.expected),
    c.test_file ? h('p', { class: 'quiet' }, 'Saved as a replayable test: ', h('code', null, c.test_file)) : null);
}

function reproduceBody(item, r, ctx) {
  const run = item.run;
  const parts = [runBlock(run, ctx)];
  if (item.msgs.length) parts.push(h('h3', null, 'Conversation with the reporter'), conversation(item.msgs, { reporterName: reporterName(r) }));
  if (item.lastRun && r.status === 'needs_info') {
    parts.push(h('p', { class: 'waiting-note' }, mark('waiting'), h('span', null, 'Waiting for the reporter to answer. Their reply puts the report back in line, and Kane tries again with what they said. You can also ', h('a', { href: '#reply' }, 'reply as the reporter'), '.')));
  }
  return h('div', null, parts);
}

function attemptBody(item, r, ctx) {
  const a = item.a;
  const parts = [];
  parts.push(h('p', { class: `attempt-status is-${a.status}` }, mark(item.state), h('span', null, ATTEMPT_LINE[a.status] || a.status)));
  if (a.status === 'running' && !a.files.length) {
    parts.push(h('p', { class: 'working' }, `${a.agent.name || 'Claude Code'} is reading Kane’s evidence and the source to write a patch. `,
      h('span', { class: 'elapsed', dataset: { elapsedFrom: a.started_at } }, duration(since(a.started_at))), ' so far.'));
    if (a.n > 1) parts.push(h('p', { class: 'quiet' }, `It has been told what attempt ${a.n - 1} changed and what Kane saw after it.`));
  }
  if (a.root_cause) parts.push(lead('Root cause', a.root_cause));
  if (a.summary) parts.push(lead('Change', a.summary));
  if (a.files.length) {
    parts.push(h('h3', null, a.files.length === 1 ? 'File changed' : `${countWord(a.files.length).replace(/^./, (c) => c.toUpperCase())} files changed`));
    parts.push(diffList(a.files, `a${a.n}`, ctx.diffState));
  }
  if (a.agent && a.agent.duration_ms) {
    parts.push(h('p', { class: 'quiet' }, `${a.agent.name} took ${duration(a.agent.duration_ms / 1000)}${a.agent.turns ? ` over ${plural(a.agent.turns, 'turn')}` : ''} and cost ${usd(a.agent.cost_usd)}.`));
  }
  if (item.verify) parts.push(runBlock(item.verify, ctx, { heading: 'Kane’s verify run' }));
  if (a.note && a.status !== 'verified') parts.push(h('p', { class: 'why' }, h('strong', null, 'Why it did not hold. '), a.note));
  return h('div', null, parts);
}

function regressBody(item, r, ctx) {
  const runs = item.runs;
  const done = runs.filter((x) => x.status !== 'running');
  const passed = done.filter((x) => x.status === 'passed').length;
  const running = runs.find((x) => x.status === 'running');
  let line;
  if (running) line = `Re-checking the checks of earlier fixes so this patch does not break them. ${passed ? `${passed} still ${passed === 1 ? 'holds' : 'hold'} so far.` : 'The first is running.'}`;
  else if (passed === runs.length) line = runs.length === 1 ? 'Re-checked the 1 earlier fix. It still holds.' : `Re-checked ${countWord(runs.length)} earlier fixes. ${runs.length === 2 ? 'Both' : 'All'} still hold.`;
  else line = `Re-checked ${plural(runs.length, 'earlier fix', 'earlier fixes')}. ${runs.length - passed} broke, so this attempt did not count.`;
  return h('div', null,
    h('p', null, line),
    h('ul', { class: 'reg-list' }, runs.map((run) => {
      const st = run.status === 'running' ? 'active' : run.status === 'passed' ? 'passed' : 'failed';
      const live = mergeSteps(run, ctx.liveSteps(run.id));
      const li = h('li', { class: `reg is-${run.status}` },
        mark(st, run.status === 'running' ? 'Running' : run.status === 'passed' ? 'Still holds' : 'Broke'),
        h('span', { class: 'reg-id' }, run.suite_test_id ? `Test ${run.suite_test_id.replace(/^t_/, '')}` : 'Test'),
        h('span', { class: 'reg-title' }, run.title),
        h('span', { class: 'reg-meta' }, run.status === 'running' ? 'Running' : `${run.status === 'passed' ? 'Still holds' : 'Broke'}, ${duration(run.duration_s)}`));
      if (run.status === 'running') {
        const ol = h('ol', { class: 'steps steps-compact', dataset: { run: run.id }, 'aria-label': `Kane's steps for ${run.title}` });
        fillSteps(ol, live);
        li.append(ol);
      } else if (run.status !== 'passed') {
        li.append(runBlock(run, ctx));
      }
      return li;
    })));
}

function proofBody(r, ctx) {
  const p = r.proof;
  const final = r.attempts.find((a) => a.n === p.attempt) || r.attempts[r.attempts.length - 1];
  const parts = [];
  parts.push(h('p', { class: 'proof-line' }, `Proven in ${duration(p.time_to_proof_s)}, on attempt ${p.attempt} of ${r.max_attempts}. Kane saw the check fail before the patch and pass after it${p.regression_total ? `, and ${p.regression_total === 1 ? 'the 1 earlier fix still holds' : `all ${countWord(p.regression_total)} earlier fixes still hold`}` : ''}.`));
  if (p.before_shot && p.after_shot) {
    parts.push(compare({
      before: p.before_shot, after: p.after_shot, beforeLabel: 'Before: Kane’s failing run', afterLabel: 'After: Kane’s passing run',
      alt: 'Screenshot of the app taken by Kane.', start: ctx.ui.get('cmp') ?? 50, onMove: (v) => ctx.ui.set('cmp', v),
    }));
  } else if (p.before_shot || p.after_shot) {
    parts.push(h('figure', { class: 'shot' }, h('div', { class: 'shot-frame' }, h('img', { class: 'shot-img', src: p.before_shot || p.after_shot, alt: p.before_shot ? 'Kane’s screenshot from the failing run.' : 'Kane’s screenshot from the passing run.' })),
      h('figcaption', { class: 'shot-cap' }, p.before_shot ? 'Only the failing run left a screenshot.' : 'Only the passing run left a screenshot.')));
  }
  if (final) {
    parts.push(lead('What was wrong', final.root_cause));
    parts.push(lead('What changed', final.summary));
  }
  const adds = p.files.reduce((s, f) => s + f.additions, 0);
  const dels = p.files.reduce((s, f) => s + f.deletions, 0);
  if (p.files.length && r.attempts.length > 1) {
    parts.push(h('h3', null, 'The whole change, from before attempt 1 to now'));
    parts.push(diffList(p.files, 'proof', ctx.diffState));
  }
  parts.push(h('div', { class: 'proof-actions' },
    h('a', { class: 'btn', href: p.patch_url || api.patchURL(r.id), download: `fixpoint-report-${r.seq}.patch` }, 'Download the patch'),
    h('span', { class: 'quiet' }, `${plural(p.files.length, 'file')}, ${adds} added, ${dels} removed.`)));
  if (p.suite_test_id) {
    parts.push(h('p', { class: 'suite-line' }, mark('passed'), h('span', null, `Added to the suite as test ${p.suite_test_id.replace(/^t_/, '')}. It re-runs after every later patch. `, h('a', { href: '/suite' }, 'See the suite'))));
  }
  return h('div', { class: 'proof' }, parts);
}

function escalatedBody(r) {
  const p = r.problem || {};
  return h('div', null,
    h('p', null, p.message || `${countWord(r.attempts.length).replace(/^./, (c) => c.toUpperCase())} attempts did not make the check pass.`),
    h('h3', null, 'What was tried'),
    h('ol', { class: 'tried' }, r.attempts.map((a) => h('li', null,
      h('strong', null, `Attempt ${a.n}. `), a.summary || ATTEMPT_LINE[a.status], a.note ? h('span', { class: 'quiet' }, ` ${a.note}`) : null))),
    h('p', null, 'Every patch was rolled back, so the app is exactly as it was before attempt 1. The diffs above are kept for reference.'),
    p.remedy ? h('div', { class: 'look-first' }, h('h3', null, 'Look at this first'), h('p', null, rich(p.remedy))) : null);
}

function errorBody(r) {
  const p = r.problem || {};
  return h('div', null,
    h('p', null, rich(p.message || 'A tool Fixpoint depends on failed.')),
    p.remedy ? h('div', { class: 'look-first' }, h('h3', null, 'What to do'), h('p', null, rich(p.remedy))) : null,
    p.retryable !== false ? h('p', { class: 'quiet' }, 'Retrying starts the report again from the beginning. If a patch was applied, it is rolled back first.') : null);
}

function approvalBody(r, ctx) {
  const btn = h('button', { type: 'button', class: 'btn btn-primary' }, 'Approve the fix');
  btn.addEventListener('click', () => ctx.act(btn, () => api.approve(r.id), 'Approved. Claude Code starts on the fix now.'));
  return h('div', null,
    h('p', null, 'Kane reproduced the problem. Autonomy is set to ask, so no file in the app is changed until someone approves a fix.'),
    h('div', { class: 'inline-actions' }, btn));
}

/** mount renders the case file for one report into root. */
export function mount(root, ctx, { id }) {
  const ui = new Map();
  const diffState = new Map();
  const viewCtx = {
    ui, diffState,
    liveSteps: (runId) => ctx.store.live.get(runId),
    act: (btn, fn, ok) => ctx.act(btn, fn, ok, () => { lastMsg = { text: ok, at: Date.now() }; ctx.refresh.report(id); }),
  };
  let lastMsg = null;
  const heading = h('h1', { tabindex: '-1', class: 'case-title' });
  const statusEl = h('p', { class: 'case-status', 'aria-live': 'polite' });
  const factsEl = h('dl', { class: 'facts case-facts' });
  const head = h('header', { class: 'case-head' }, h('p', { class: 'back' }, h('a', { href: '/' }, 'All reports')), heading, statusEl, factsEl);
  const timeline = h('div', { class: 'timeline' });
  const plot = createCobweb();
  const actions = h('div', { class: 'actions' });
  const side = h('aside', { class: 'case-side', 'aria-label': 'Plot, actions and facts' }, plot.el, actions);
  const facts2 = h('dl', { class: 'facts side-facts' });
  side.append(h('h2', { class: 'side-h' }, 'Facts'), facts2);
  const msg = h('div', { class: 'view-msg' });
  const page = h('div', { class: 'case' }, head, side, h('div', { class: 'case-main' }, h('h2', { class: 'visually-hidden' }, 'What happened, in order'), timeline));
  root.append(msg, page);
  page.hidden = true;
  let lastStatus = null;
  let wantFocus = false;
  let actionsSig = '';
  let loadingTimer = setTimeout(() => {
    if (!ctx.store.details.has(id)) msg.replaceChildren(h('p', { class: 'loading' }, 'Loading the case file.'));
  }, 250);

  ctx.refresh.report(id);

  function renderActions(r) {
    const sig = JSON.stringify([r.status, r.attempts.length, !!r.proof, r.problem && r.problem.retryable]);
    if (sig === actionsSig) return;
    actionsSig = sig;
    const list = [];
    if (r.status === 'awaiting_approval') {
      const b = h('button', { type: 'button', class: 'btn btn-primary' }, 'Approve the fix');
      b.addEventListener('click', () => viewCtx.act(b, () => api.approve(r.id), 'Approved. Claude Code starts on the fix now.'));
      list.push(b);
    }
    const retryable = ['error', 'escalated', 'unreproduced', 'needs_info'].includes(r.status) && !(r.status === 'error' && r.problem && r.problem.retryable === false);
    if (retryable) {
      const b = h('button', { type: 'button', class: r.status === 'error' || r.status === 'escalated' ? 'btn btn-primary' : 'btn' }, 'Retry this report');
      b.addEventListener('click', () => viewCtx.act(b, () => api.retry(r.id), 'Retried. The report is back in line.'));
      list.push(b);
    }
    list.push(copyButton('Copy the status link', () => api.statusURL(r.id)));
    if (r.proof || r.attempts.some((a) => a.files.length)) {
      list.push(h('a', { class: 'btn', href: api.patchURL(r.id), download: `fixpoint-report-${r.seq}.patch` }, r.proof ? 'Download the patch' : 'Download the latest patch'));
    }
    const parts = [h('div', { class: 'action-buttons' }, list)];
    if (r.status === 'needs_info') parts.unshift(replyForm(r));
    parts.push(h('p', { class: 'action-msg', role: 'status' }, lastMsg && Date.now() - lastMsg.at < 8000 ? lastMsg.text : ''));
    actions.replaceChildren(h('h2', { class: 'side-h' }, 'Actions'), ...parts);
  }

  function replyForm(r) {
    const q = [...r.thread].reverse().find((m) => m.from === 'fixpoint');
    const ta = h('textarea', { id: 'reply', name: 'reply', rows: 4, required: true, 'aria-describedby': 'reply-help' });
    const err = h('p', { class: 'form-error', id: 'reply-err', role: 'alert' });
    const btn = h('button', { type: 'submit', class: 'btn btn-primary' }, 'Send the reply');
    const form = h('form', { class: 'reply-form' },
      h('label', { for: 'reply' }, 'Reply as the reporter'),
      h('p', { class: 'quiet', id: 'reply-help' }, q ? `Answering: ${q.text}` : 'Your answer joins the conversation and puts the report back in line.'),
      ta, err, btn);
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      const text = ta.value.trim();
      if (text.length < 2) { err.textContent = 'Write an answer before sending it.'; ta.focus(); return; }
      err.textContent = '';
      btn.disabled = true;
      btn.textContent = 'Sending';
      try {
        await api.reply(r.id, text);
        ta.value = '';
        announce('Reply sent. The report is back in line.');
        ctx.refresh.report(id);
      } catch (ex) {
        err.textContent = ex.message;
      } finally {
        btn.disabled = false;
        btn.textContent = 'Send the reply';
      }
    });
    return form;
  }

  function renderHead(r) {
    document.title = `#${r.seq} ${r.title} | Fixpoint`;
    heading.replaceChildren(h('span', { class: 'case-num' }, `#${r.seq}`), ' ', r.title);
    const just = lastStatus && lastStatus !== 'proven' && r.status === 'proven';
    statusEl.replaceChildren(mark(markFor(r.status)), h('span', null, statusText(r)));
    if (r.simulated) statusEl.append(h('span', { class: 'simulated-tag' }, 'Simulated'));
    statusEl.classList.toggle('just-proven', !!just);
    const f = [];
    const active = ACTIVE.includes(r.status);
    if (r.proof) f.push(['Time to proof', duration(r.proof.time_to_proof_s)]);
    else if (active) f.push(['Running for', h('span', { class: 'elapsed', dataset: { elapsedFrom: r.created_at } }, duration(since(r.created_at)))]);
    f.push(['Attempts', r.attempts.length ? `${r.attempts.length} of ${r.max_attempts}` : 'None yet']);
    const cr = r.runs.reduce((s, x) => s + (x.credits || 0), 0);
    f.push(['Kane credits', credits(cr)]);
    const cost = r.attempts.reduce((s, a) => s + ((a.agent && a.agent.cost_usd) || 0), 0);
    if (cost) f.push(['Agent cost', usd(cost)]);
    factsEl.replaceChildren(...f.map(([k, v]) => h('div', null, h('dt', null, k), h('dd', null, v))));
    lastStatus = r.status;

    const c = r.context || {};
    const rows = [
      ['Filed', dateTime(r.created_at)],
      ['Reporter', r.reporter && (r.reporter.name || r.reporter.email) ? [r.reporter.name, r.reporter.email].filter(Boolean).join(', ') : 'Not given'],
      ['Browser', browser(c.user_agent) || 'Unknown'],
      ['Kane runs', String(r.runs.length)],
      ['Check', r.check ? `Revision ${r.check.revision}, by ${r.check.written_by}` : 'Not written yet'],
      ['Report id', h('code', null, r.id)],
    ];
    if (r.closed_at) rows.splice(1, 0, ['Closed', dateTime(r.closed_at)]);
    facts2.replaceChildren(...rows.map(([k, v]) => h('div', null, h('dt', null, k), h('dd', null, v))));
  }

  function update() {
    const d = ctx.store.details.get(id);
    const err = ctx.store.detailErrors.get(id);
    if (!d) {
      if (err) {
        clearTimeout(loadingTimer);
        page.hidden = true;
        document.title = 'Report not found | Fixpoint';
        heading.textContent = '';
        msg.replaceChildren(
          h('h1', { tabindex: '-1', class: 'msg-title' }, err.status === 404 ? 'There is no report at this address' : 'This report could not be loaded'),
          h('p', null, err.status === 404 ? 'Check the link, or find the report on the board. Resetting the demo removes every report.' : rich(err.message)),
          h('p', { class: 'inline-actions' }, h('a', { class: 'btn', href: '/' }, 'Back to all reports'),
            err.status !== 404 ? Object.assign(h('button', { type: 'button', class: 'btn' }, 'Try again'), { onclick: () => ctx.refresh.report(id) }) : null));
        if (wantFocus) { wantFocus = false; msg.querySelector('h1').focus({ preventScroll: true }); }
      }
      return;
    }
    clearTimeout(loadingTimer);
    msg.replaceChildren();
    page.hidden = false;
    const r = d.report;
    renderHead(r);
    if (wantFocus) { wantFocus = false; heading.focus({ preventScroll: true }); }
    plot.update(r);
    renderActions(r);
    const items = plan(r, d.events || []);
    reconcile(timeline, items, {
      key: (x) => x.key,
      sig: (x) => sigOf(x, r),
      render: (x) => section({ key: x.key, title: x.title, state: x.state, at: x.at, offset: x.key === 'reported' ? '' : offsetFrom(r, x.at), body: body(x, r, viewCtx), ui }),
    });
    syncSteps(r);
  }

  function syncSteps(r) {
    for (const ol of timeline.querySelectorAll('ol.steps[data-run]')) {
      const run = r.runs.find((x) => x.id === ol.dataset.run);
      if (run) fillSteps(ol, mergeSteps(run, ctx.store.live.get(run.id)));
    }
  }

  function onStep(ev) {
    if (ev.report_id !== id) return;
    const d = ctx.store.details.get(id);
    if (!d) return;
    const run = d.report.runs.find((x) => x.id === ev.data.run_id);
    const ol = timeline.querySelector(`ol.steps[data-run="${CSS.escape(String(ev.data.run_id))}"]`);
    if (run && ol) fillSteps(ol, mergeSteps({ ...run, status: 'running' }, ctx.store.live.get(run.id)));
  }

  return {
    heading: () => {
      if (!page.hidden) return heading;
      wantFocus = true;
      return msg.querySelector('h1');
    },
    title: 'Report | Fixpoint',
    update,
    onStep,
    wants: (reportId) => reportId === id,
    unmount() { clearTimeout(loadingTimer); },
  };
}
