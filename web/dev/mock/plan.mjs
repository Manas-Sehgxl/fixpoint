// Turns a scenario and a list of phases into a timed script. A script is a
// list of entries; each has a time offset, the core.Event it emits, a pure
// change to the report, and optionally a one-off effect on the world (such
// as adding a suite test). Replaying the changes up to a moment rebuilds the
// report as it was then, which is how both the static fixtures (played to the
// end, long ago) and the live report (played in real time) are made.

const AGENT = 'Claude Code';

export const artifacts = new Map();

export function fmtDuration(s) {
  s = Math.round(s);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60), r = s % 60;
  if (m < 60) return `${m}m ${String(r).padStart(2, '0')}s`;
  return `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, '0')}m`;
}

function shotSpec(scn, k, n, outcome, annotated) {
  if (k === n) {
    const base = outcome === 'failed' ? scn.shotFail : scn.shotPass;
    return { ...base, step: k, of: n, tone: outcome === 'failed' ? 'fail' : 'pass', note: annotated ? base.note : '' };
  }
  const items = k === 1 ? [] : (scn.shotItems || []).slice(0, k - 1);
  const search = scn.shotFail && scn.shotFail.search && k > 1 ? scn.shotFail.search : '';
  return { step: k, of: n, items, cart: [], mark: -1, tone: 'plain', search };
}

class Script {
  constructor(id, scale, runN) {
    this.id = id;
    this.scale = scale;
    this.t = 0;
    this.list = [];
    this.runN = runN || 0;
  }
  wait(dt) { this.t += dt * this.scale; }
  waitRaw(dt) { this.t += dt; }
  add(type, stage, text, data, mut, effect) {
    this.list.push({ t: this.t, ev: { report_id: this.id, type, stage, text, data: data || {} }, mut, effect });
  }
}

function setStatus(S, status, stage, text, extra) {
  S.add('report.status', stage, text, { status }, (r) => { r.status = status; if (extra) extra(r); });
}

function runPhase(S, o) {
  const runId = `run_${++S.runN}`;
  const n = o.remarks.length;
  const outcome = o.outcome;
  S.add('run.started', o.stage, o.startText, { run_id: runId, kind: o.kind, attempt: o.attempt, title: o.title }, (r, at) => {
    r.runs.push({
      id: runId, kind: o.kind, attempt: o.attempt, suite_test_id: o.suiteTestId || '', title: o.title,
      objective: o.objective, status: 'running', exit_code: 0, summary: '', one_liner: '', reason: '',
      duration_s: 0, credits: 0, final_state: {}, test_url: '', steps: [], cli_version: '0.10.0',
      simulated: false, error: '', started_at: at,
    });
  });
  const started = S.t;
  if (outcome === 'error') {
    S.wait(1.4);
  }
  for (let k = 1; outcome !== 'error' && k <= n; k++) {
    S.wait(0.3);
    const remark = o.remarks[k - 1];
    S.add('run.step', o.stage, remark, { run_id: runId, n: k, status: 'running', remark }, (r) => {
      r.runs.find((x) => x.id === runId).steps.push({ n: k, status: 'running', remark });
    });
    S.wait(o.stepTime || 0.85);
    const st = outcome === 'failed' && k === n ? 'failed' : 'done';
    S.add('run.step', o.stage, remark, { run_id: runId, n: k, status: st, remark }, (r) => {
      r.runs.find((x) => x.id === runId).steps.find((x) => x.n === k).status = st;
    });
  }
  S.wait(0.4);
  const dur = S.t - started;
  const shots = [];
  for (let k = 1; outcome !== 'error' && k <= n; k++) {
    const base = `/artifacts/${S.id}/${runId}/step-${k}`;
    artifacts.set(`${base}.png`, shotSpec(o.scn, k, n, outcome, false));
    const annotated = k === n ? `${base}-annotated.png` : '';
    if (annotated) artifacts.set(annotated, shotSpec(o.scn, k, n, outcome, true));
    shots.push({ step: k, url: `${base}.png`, annotated_url: annotated, failed: outcome === 'failed' && k === n });
  }
  const failed = outcome === 'failed';
  const reason = failed ? `assertion_failed: @ step ${n}` : outcome === 'error' ? 'kane_exit_2' : '';
  const credits = outcome === 'error' ? 0 : Math.round((n * 1.9 + (o.kind === 'reproduce' ? 1.1 : 0.4)) * 10) / 10;
  S.add('run.finished', o.stage, o.endText, { run_id: runId, kind: o.kind, status: outcome, reason }, (r, at) => {
    const run = r.runs.find((x) => x.id === runId);
    Object.assign(run, {
      status: outcome,
      exit_code: failed ? 1 : outcome === 'error' ? 2 : 0,
      summary: failed ? o.scn.failSummary : outcome === 'passed' ? o.scn.passSummary : '',
      one_liner: failed ? o.scn.failSummary.split(';')[0] : outcome === 'passed' ? 'Check passed.' : '',
      reason,
      duration_s: Math.round(dur * 10) / 10,
      credits,
      final_state: outcome === 'error' ? {} : { observed: failed ? o.scn.observedFail : o.scn.observedPass },
      test_url: outcome === 'error' ? '' : `https://test-manager.testmuai.com/runs/${S.id.slice(2)}-${runId}`,
      error: outcome === 'error' ? o.scn.runError : '',
      ended_at: at,
    });
    if (failed && o.kind === 'reproduce' && o.scn.verdict) run.bug_verdict = o.scn.verdict;
    if (outcome !== 'error') {
      run.evidence = {
        pack_path: `/home/you/.fixpoint/kane/.testmuai/evidence/${S.id}-${runId}.evidence`,
        shots,
        failure_text: failed ? `step: ${n}\nassertion: ${o.remarks[n - 1]}\nobserved: ${o.scn.observedFail}\nresult: failed\n` : '',
        console_errors: failed ? (o.scn.context.console_errors || []).slice(0, 2) : [],
      };
    }
  }, o.suiteTestId ? (world, at, r) => world.suiteResult(o.suiteTestId, r.runs.find((x) => x.id === runId)) : undefined);
  return runId;
}

function intake(S, scn, wait) {
  const who = scn.reporter.name || scn.reporter.email;
  S.add('report.created', '', who ? `Report received from ${who}.` : 'Report received.', {}, (r) => { r.status = 'queued'; });
  S.waitRaw(wait || 0);
  triage(S, scn, true);
}

function triage(S, scn, first) {
  S.wait(1.4);
  setStatus(S, 'triaging', 'triage', `${AGENT} is reading the report and writing a check.`);
  S.wait(3.2);
  if (!first) return;
  S.add('check.written', 'triage', `${AGENT} wrote a check that passes only when ${lower(scn.expected)}`, { revision: 1 }, (r) => {
    r.title = scn.title;
    r.check = {
      title: scn.title, objective: scn.objective, expected: scn.expected, start_url: 'http://localhost:4000',
      written_by: AGENT, revision: 1, test_file: '',
    };
  });
}

function lower(s) { return s.charAt(0).toLowerCase() + s.slice(1); }

function reproduce(S, scn, outcome) {
  S.wait(0.4);
  setStatus(S, 'reproducing', 'reproduce', 'Kane is running the check in a real browser.');
  const text = {
    failed: `Kane reproduced it: ${lower(scn.failSummary || '')}`,
    passed: `Kane could not reproduce it: ${lower(scn.passSummary || '')}`,
    error: 'Kane CLI could not start, so the check did not run.',
  }[outcome];
  return runPhase(S, {
    kind: 'reproduce', attempt: 0, stage: 'reproduce', title: scn.title, objective: scn.objective,
    remarks: scn.remarks, outcome, scn, startText: 'Kane started the first run of the check.', endText: text,
  });
}

function attempt(S, scn, n, max, outcome, regressions) {
  const a = scn.attempts[n - 1];
  S.wait(0.4);
  S.add('report.status', 'fix', `Attempt ${n} of ${max}: ${AGENT} is working on a fix.`, { status: 'fixing' }, (r) => { r.status = 'fixing'; });
  S.add('attempt.started', 'fix', `Attempt ${n} started.`, { n }, (r, at) => {
    r.attempts.push({
      n, status: 'running', root_cause: '', summary: '', note: '',
      agent: { name: AGENT, duration_ms: 0, cost_usd: 0, turns: 0, session_id: '' },
      files: [], verify_run_id: '', regression_run_ids: [], started_at: at,
    });
  });
  S.waitRaw(a.agent.duration_ms / 1000);
  const adds = a.files.reduce((s, f) => s + f.additions, 0);
  const dels = a.files.reduce((s, f) => s + f.deletions, 0);
  const fileWord = a.files.length === 1 ? '1 file' : `${a.files.length} files`;
  S.add('attempt.patched', 'fix', `Attempt ${n} changed ${fileWord}: ${a.summary}`, { n, files: a.files.length, additions: adds, deletions: dels }, (r) => {
    Object.assign(r.attempts[n - 1], {
      root_cause: a.root_cause, summary: a.summary, files: a.files,
      agent: { name: AGENT, ...a.agent, session_id: `sess_${S.id.slice(2, 8)}${n}` },
    });
  });
  S.wait(0.3);
  setStatus(S, 'verifying', 'verify', 'Kane is running the same check against the patched app.');
  const vOutcome = outcome === 'still_failing' ? 'failed' : 'passed';
  const vid = runPhase(S, {
    kind: 'verify', attempt: n, stage: 'verify', title: scn.title, objective: scn.objective, remarks: scn.remarks,
    outcome: vOutcome, scn,
    startText: `Kane is checking attempt ${n}.`,
    endText: vOutcome === 'passed' ? 'The check passes now.' : `The check still fails. ${a.note || ''}`.trim(),
  });
  const link = S.list[S.list.length - 1];
  const prevMut = link.mut;
  link.mut = (r, at) => { prevMut(r, at); r.attempts[n - 1].verify_run_id = vid; };

  if (vOutcome === 'passed' && regressions.length) {
    S.wait(0.3);
    const word = regressions.length === 1 ? 'the 1 earlier fix' : `the ${regressions.length} earlier fixes`;
    setStatus(S, 'regressing', 'regress', `Re-checking ${word} so nothing else broke.`);
    regressions.forEach((t, i) => {
      const bad = outcome === 'regressed' && i === 0;
      const rid = runPhase(S, {
        kind: 'regression', attempt: n, stage: 'regress', title: t.title, objective: t.objective,
        remarks: t.scn.remarks.slice(0, 4).concat(t.scn.remarks.slice(-1)), outcome: bad ? 'failed' : 'passed', scn: t.scn,
        suiteTestId: t.id, stepTime: 0.55,
        startText: `Re-checking test ${t.id.slice(2)}: ${t.title}.`,
        endText: bad ? `Test ${t.id.slice(2)} broke: ${t.title}.` : `Still holds: ${t.title}.`,
      });
      const last = S.list[S.list.length - 1];
      const m = last.mut;
      last.mut = (r, at) => { m(r, at); r.attempts[n - 1].regression_run_ids.push(rid); };
    });
  }
  S.wait(0.3);
  const word = { verified: 'held', still_failing: 'did not hold', regressed: 'broke an earlier fix' }[outcome];
  S.add('attempt.finished', 'fix', `Attempt ${n} ${word}.`, { n, status: outcome }, (r, at) => {
    Object.assign(r.attempts[n - 1], { status: outcome, note: outcome === 'verified' ? '' : (a.note || ''), ended_at: at });
  });
  return vid;
}

/**
 * build makes the script for one report.
 * phases: list of { reproduce }, { attempt, outcome }, { prove }, { escalate },
 *         { ask }, { reply }, { close }, { await }, { error }.
 */
export function build(scn, phases, o) {
  const S = new Script(o.id, o.scale || 1, o.runN);
  const max = o.maxAttempts || 3;
  let redRun = '', greenRun = '', regressions = o.regressions || [];
  for (const p of phases) {
    if (p.intake) intake(S, scn, p.wait);
    if (p.retry) {
      S.add('report.requeued', '', 'Retried. Back in line from the start.', {}, (r) => {
        r.status = 'queued';
        delete r.problem;
        delete r.closed_at;
        delete r.proof;
        r.runs = [];
        r.attempts = [];
      });
      triage(S, scn, false);
    }
    if (p.resume) {
      S.add('report.status', 'fix', 'Approved. The fix can start.', { status: 'fixing' }, (r) => { r.status = 'fixing'; });
      redRun = p.redRun;
    }
    if (p.reproduce) {
      const id = reproduce(S, scn, p.reproduce);
      if (p.reproduce === 'failed') redRun = id;
    }
    if (p.await) {
      S.wait(0.3);
      setStatus(S, 'awaiting_approval', 'fix', 'Reproduced. Waiting for someone to approve a fix.');
    }
    if (p.attempt) {
      const id = attempt(S, scn, p.attempt, max, p.outcome, regressions);
      if (p.outcome === 'verified') greenRun = id;
    }
    if (p.ask) {
      S.wait(0.4);
      S.add('report.question', 'reproduce', `Asked the reporter: ${p.ask}`, { question: p.ask }, (r, at) => {
        r.status = 'needs_info';
        r.thread.push({ from: 'fixpoint', text: p.ask, at });
      });
    }
    if (p.reply) {
      S.waitRaw(p.after || 0);
      S.add('report.reply', '', 'The reporter replied.', {}, (r, at) => { r.thread.push({ from: 'reporter', text: p.reply, at }); });
      S.wait(0.2);
      S.add('report.requeued', '', 'Back in line with the reporter\'s answer.', {}, (r) => { r.status = 'queued'; });
      triage(S, scn, false);
    }
    if (p.close) {
      S.waitRaw(p.after || 0);
      S.add('report.status', '', 'Closed: the app behaved correctly every time Kane tried it.', { status: 'unreproduced' }, (r, at) => {
        r.status = 'unreproduced';
        r.closed_at = at;
      });
    }
    if (p.error) {
      S.wait(0.2);
      S.add('report.error', 'reproduce', scn.problem.message, {}, (r) => { r.status = 'error'; r.problem = scn.problem; });
    }
    if (p.escalate) {
      S.wait(0.4);
      S.add('report.escalated', 'fix', `${max} attempts did not hold. The patches were rolled back and a person has the case.`, {}, (r, at) => {
        r.status = 'escalated';
        r.problem = scn.escalation;
        r.closed_at = at;
      });
    }
    if (p.prove) {
      S.wait(0.6);
      const testId = p.testId;
      const final = scn.attempts.filter((a) => a).slice(-1)[0];
      const files = scn.proofFiles || final.files;
      const red = redRun, green = greenRun;
      const n = phases.filter((x) => x.attempt).slice(-1)[0].attempt;
      S.add('report.proven', 'regress', '', {}, (r, at) => {
        const ttp = (Date.parse(at) - Date.parse(r.created_at)) / 1000;
        r.status = 'proven';
        r.closed_at = at;
        const lastShot = (id) => {
          const run = r.runs.find((x) => x.id === id);
          const shots = (run && run.evidence && run.evidence.shots) || [];
          return shots.length ? shots[shots.length - 1].url : '';
        };
        const regs = r.runs.filter((x) => x.kind === 'regression' && x.attempt === n);
        r.check.test_file = `.fixpoint/kane/.testmuai/tests/${scn.key.replace(/-/g, '_')}_test.md`;
        r.proof = {
          red_run_id: red, green_run_id: green, attempt: n, time_to_proof_s: Math.round(ttp * 10) / 10,
          files, patch_url: `/api/reports/${r.id}/patch`, before_shot: lastShot(red), after_shot: lastShot(green),
          suite_test_id: testId, regression_total: regs.length, regression_passed: regs.filter((x) => x.status === 'passed').length,
        };
      }, (world, at, r) => {
        world.addSuiteTest({ id: testId, scn, report: r, at });
      });
      const last = S.list[S.list.length - 1];
      last.textFor = (r) => `Proven in ${fmtDuration(r.proof.time_to_proof_s)}. The check joins the suite as test ${testId.slice(2)}.`;
    }
  }
  return S.list;
}

/** track derives the five-node track from a report, following SPEC section 10. */
export function track(r) {
  const keys = ['reported', 'reproduced', 'patched', 'verified', 'proven'];
  const labels = ['Reported', 'Reproduced', 'Patched', 'Verified', 'Proven'];
  const st = {
    queued: ['active'], triaging: ['active'],
    reproducing: ['done', 'active'],
    needs_info: ['done', 'waiting'],
    awaiting_approval: ['done', 'done', 'waiting'],
    fixing: ['done', 'done', 'active'],
    verifying: ['done', 'done', 'done', 'active'],
    regressing: ['done', 'done', 'done', 'active'],
    proven: ['done', 'done', 'done', 'done', 'done'],
    unreproduced: ['done'],
    escalated: ['done', 'done', 'done', 'failed'],
    error: null,
  }[r.status];
  let states = st;
  if (!states) {
    const stage = (r.problem && r.problem.stage) || 'reproduce';
    const at = { triage: 0, reproduce: 1, fix: 2, verify: 3, regress: 3 }[stage] ?? 1;
    states = keys.map((_, i) => (i < at ? 'done' : i === at ? 'failed' : 'pending'));
  }
  return keys.map((key, i) => ({ key, label: labels[i], state: states[i] || 'pending' }));
}

const VIEW = {
  queued: ['received', 1, 'Report received', 'It is in line. This page updates by itself.'],
  triaging: ['received', 1, 'Reading your report', 'Working out exactly what to test.'],
  reproducing: ['reproducing', 2, 'Trying it in a real browser', 'We are repeating what you did to see the problem ourselves.'],
  needs_info: ['question', 2, 'We could not reproduce it yet', ''],
  awaiting_approval: ['fixing', 2, 'We saw the problem too', 'A developer is about to start on a fix.'],
  fixing: ['fixing', 3, 'We saw the problem too', 'A fix is being written.'],
  verifying: ['verifying', 3, 'Testing the fix', 'The same steps are being run again in a real browser.'],
  regressing: ['verifying', 3, 'Testing the fix', 'Making sure nothing else broke.'],
  proven: ['fixed', 4, 'Fixed and verified', 'Reload the page to get the fix. Thank you for reporting it.'],
  unreproduced: ['closed', 2, 'We could not reproduce this', 'We tried, and the app behaved correctly for us. Send a new report if it happens again.'],
  escalated: ['handed_off', 3, 'A developer is taking this one', 'Automatic fixes did not hold, so a person has everything we found.'],
  error: ['trouble', 1, 'Something went wrong on our side', 'The team has been told. Nothing more is needed from you.'],
};

/** publicView derives the reporter-facing wording, following SPEC section 10. */
export function publicView(r) {
  const [stage, step, headline, detail] = VIEW[r.status];
  const q = r.status === 'needs_info' ? ([...r.thread].reverse().find((m) => m.from === 'fixpoint') || {}).text || '' : '';
  return {
    stage, step, headline, detail: detail || q, question: q,
    before_shot: (r.proof && r.proof.before_shot) || '',
    after_shot: (r.proof && r.proof.after_shot) || '',
  };
}
