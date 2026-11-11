#!/usr/bin/env node
// Fixpoint mock server, for building the console before the engine exists.
// Zero dependencies. Serves web/console/ and the HTTP API of SPEC section 10
// from scripted fixtures, with a live report that plays from queued to proven
// in about 40 seconds.
//
//   node web/dev/mock/server.mjs --port 7801 [--scenario simulated]
//
// Open http://localhost:7801/__mock for links to every state. Any console page
// accepts ?scenario=<name>, ?freeze=<seconds> and ?restart; the choice is kept
// in a cookie so the console's own API calls see the same world.

import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { build, track, publicView, artifacts, fmtDuration } from './plan.mjs';
import { fixtures, LIVE_ID } from './fixtures.mjs';
import { freeDelivery } from './scenarios.mjs';
import { shot } from './png.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(here, '../../console');

const args = process.argv.slice(2);
const arg = (name, def) => {
  const i = args.indexOf(`--${name}`);
  return i >= 0 && args[i + 1] ? args[i + 1] : def;
};
const PORT = Number(arg('port', process.env.PORT || 7801));
const DEFAULT_SCENARIO = arg('scenario', process.env.MOCK_SCENARIO || 'default');

export const SCENARIOS = {
  default: 'Everything ready; eight reports, one playing live.',
  empty: 'A fresh install: no reports and an empty suite.',
  simulated: 'Stub runners are on, so every surface says runs are simulated.',
  'kane-missing': 'Kane CLI is not installed.',
  'signed-out': 'Kane CLI is installed but not signed in.',
  'target-down': 'The demo shop does not answer.',
  ask: 'Autonomy is "ask": the live report stops for approval after Kane reproduces it.',
  slow: 'Every API answer takes six seconds, to see loading states.',
  down: 'The API answers 503 and the event stream refuses connections.',
};

const iso = (ms) => new Date(ms).toISOString();

function skeleton(f, createdMs) {
  return {
    id: f.id, seq: f.seq, status: 'queued', title: f.scn.text.split(/\s+/).slice(0, 8).join(' '),
    text: f.scn.text, reporter: f.scn.reporter, context: f.scn.context, thread: [],
    runs: [], attempts: [], max_attempts: 3, queue_position: 0, simulated: false,
    track: [], public: {}, created_at: iso(createdMs), updated_at: iso(createdMs),
  };
}

class World {
  constructor(name, freeze) {
    this.name = name;
    this.freeze = freeze;
    this.seq = 0;
    this.log = [];
    this.clients = new Set();
    this.publicClients = new Set();
    this.suite = [];
    this.suiteRunning = false;
    this.suiteTimers = [];
    this.instances = [];
    this.testScn = {};
    this.nextTest = 1;
    if (name === 'empty') return;

    const now = Date.now();
    const fx = fixtures();
    for (const f of fx) for (const p of f.phases) if (p.prove && p.testId !== 'next') this.testScn[p.testId] = f.scn;
    // Static fixtures happened long ago: build them, run their effects in time order.
    const past = [];
    for (const f of fx.filter((x) => !x.live)) {
      const inst = this.instance(f, now - f.agoMin * 60000, f.phases, skeleton(f, now - f.agoMin * 60000));
      for (const e of inst.entries) past.push({ inst, e });
    }
    past.sort((a, b) => (a.inst.start + a.e.t * 1000) - (b.inst.start + b.e.t * 1000));
    for (const { inst, e } of past) this.emit(inst, e, inst.start + e.t * 1000, false);
    const live = fx.find((x) => x.live);
    if (live) this.startLive(live);
  }

  regressionsFor(ids) {
    const tests = ids ? ids.map((id) => ({ id, title: this.testScn[id].title, objective: this.testScn[id].objective, scn: this.testScn[id] }))
      : this.suite.map((t) => ({ id: t.id, title: t.title, objective: t.objective, scn: this.testScn[t.id] }));
    return tests.filter((t) => t.scn);
  }

  instance(f, start, phases, base, extra = {}) {
    const resolved = phases.map((p) => (p.prove && p.testId === 'next' ? { ...p, testId: f.reservedTest || (f.reservedTest = `t_${this.nextFreeTest()}`) } : p));
    const entries = build(f.scn, resolved, {
      id: f.id, scale: extra.scale ?? f.scale, regressions: this.regressionsFor(extra.useSuite ? null : f.regressions),
      runN: base.runs.length,
    });
    let inst = this.instances.find((x) => x.f.id === f.id);
    if (!inst) { inst = { f }; this.instances.push(inst); }
    Object.assign(inst, { start, entries, base, timers: [], live: !!extra.live });
    return inst;
  }

  nextFreeTest() {
    const used = new Set([...this.suite.map((t) => t.id), ...this.instances.map((i) => i.f.reservedTest).filter(Boolean), ...Object.keys(this.testScn)]);
    let n = 1;
    while (used.has(`t_${n}`)) n++;
    return n;
  }

  startLive(f, delay = 1200) {
    const old = this.instances.find((x) => x.f.id === f.id);
    if (old) this.stop(old);
    this.log = this.log.filter((e) => e.report_id !== f.id);
    this.suite = this.suite.filter((t) => t.report_id !== f.id);
    const start = Date.now() + delay;
    const inst = this.instance(f, start, this.phasesFor(f), skeleton(f, start), { live: true });
    if (this.freeze != null) {
      inst.frozen = true;
      for (const e of inst.entries) if (e.t <= this.freeze) this.emit(inst, e, Date.now() - (this.freeze - e.t) * 1000, false);
      return inst;
    }
    this.schedule(inst);
    return inst;
  }

  phasesFor(f) {
    if (this.name === 'ask' && f.live) return [{ intake: true, wait: 0.8 }, { reproduce: 'failed' }, { await: true }];
    return f.phases;
  }

  stop(inst) { inst.timers.forEach(clearTimeout); inst.timers = []; }

  schedule(inst) {
    const now = Date.now();
    for (const e of inst.entries) {
      const when = inst.start + e.t * 1000;
      inst.timers.push(setTimeout(() => this.emit(inst, e, when, true), Math.max(0, when - now)));
    }
  }

  // continueWith replaces an instance's script with new phases starting now,
  // on top of the report as it stands.
  continueWith(inst, phases, extra) {
    this.stop(inst);
    const base = this.reportAt(inst, Date.now());
    delete base.track;
    delete base.public;
    const start = Date.now() + 300;
    this.instance(inst.f, start, phases, base, { ...extra, live: true });
    this.schedule(inst);
  }

  emit(inst, e, when, broadcast) {
    const r = this.reportAt(inst, when + 1);
    if (e.effect) e.effect(this, iso(when), r);
    const ev = { seq: ++this.seq, ...e.ev, text: e.textFor ? e.textFor(r) : e.ev.text, at: iso(when) };
    this.log.push(ev);
    if (broadcast) this.broadcast(ev);
  }

  startOf(inst, now) {
    return inst.frozen ? now - this.freeze * 1000 + 1 : inst.start;
  }

  reportAt(inst, now) {
    const r = structuredClone(inst.base);
    const start = this.startOf(inst, now);
    for (const e of inst.entries) {
      const when = start + e.t * 1000;
      if (when > now) break;
      if (e.mut) e.mut(r, iso(when));
      r.updated_at = iso(when);
    }
    return r;
  }

  report(inst, now = Date.now()) {
    const r = this.reportAt(inst, now);
    if (inst.frozen) r.created_at = iso(this.startOf(inst, now));
    r.track = track(r);
    r.public = publicView(r);
    r.simulated = this.name === 'simulated';
    for (const run of r.runs) run.simulated = r.simulated;
    if (r.status === 'queued') {
      const queued = this.reports(now, true).filter((x) => x.status === 'queued').sort((a, b) => a.created_at.localeCompare(b.created_at));
      r.queue_position = queued.findIndex((x) => x.id === r.id) + 1;
    }
    return r;
  }

  reports(now = Date.now(), raw = false) {
    const list = this.instances
      .filter((i) => this.startOf(i, now) <= now)
      .map((i) => (raw ? this.reportAt(i, now) : this.report(i, now)));
    return list.sort((a, b) => b.seq - a.seq);
  }

  find(id) { return this.instances.find((i) => i.f.id === id && this.startOf(i, Date.now()) <= Date.now()); }

  addSuiteTest({ id, scn, report, at }) {
    this.testScn[id] = scn;
    if (this.suite.some((t) => t.id === id)) return;
    const green = report.runs.find((x) => x.id === report.proof?.green_run_id) || report.runs.filter((x) => x.kind === 'verify').pop();
    this.suite.push({
      id, title: scn.title, objective: scn.objective, start_url: 'http://localhost:4000',
      test_file: `.fixpoint/kane/.testmuai/tests/${scn.key.replace(/-/g, '_')}_test.md`,
      report_id: report.id, report_seq: report.seq, created_at: at,
      last_run: green ? { status: 'passed', reason: '', duration_s: green.duration_s, replayed: false, at } : undefined,
    });
  }

  suiteResult(id, run) {
    const t = this.suite.find((x) => x.id === id);
    if (t && run) t.last_run = { status: run.status, reason: run.reason, duration_s: run.duration_s, replayed: true, at: run.ended_at };
  }

  runSuite() {
    if (this.suiteRunning) return false;
    this.suiteRunning = true;
    const sys = (type, text, data) => {
      const ev = { seq: ++this.seq, report_id: '', type, stage: '', text, data, at: iso(Date.now()) };
      this.log.push(ev);
      this.broadcast(ev);
    };
    sys('suite.started', `Running all ${this.suite.length} checks.`, {});
    let t = 600;
    for (const test of [...this.suite]) {
      this.suiteTimers.push(setTimeout(() => sys('suite.test', `Running test ${test.id.slice(2)}: ${test.title}.`, { test_id: test.id, status: 'running' }), t));
      t += 2600 + Math.round(test.title.length * 15);
      const end = t;
      this.suiteTimers.push(setTimeout(() => {
        test.last_run = { status: 'passed', reason: '', duration_s: Math.round((end - (end - 2600)) / 100) / 10 + 0.4, replayed: true, at: iso(Date.now()) };
        sys('suite.test', `Still holds: ${test.title}.`, { test_id: test.id, status: 'passed' });
      }, t));
      t += 300;
    }
    this.suiteTimers.push(setTimeout(() => {
      this.suiteRunning = false;
      sys('suite.finished', `All ${this.suite.length} checks passed.`, { passed: this.suite.length, failed: 0 });
    }, t + 200));
    return true;
  }

  reset() {
    for (const i of this.instances) this.stop(i);
    this.suiteTimers.forEach(clearTimeout);
    this.instances = [];
    this.suite = [];
    this.suiteRunning = false;
    this.log = [];
    const ev = { seq: ++this.seq, report_id: '', type: 'system.changed', stage: '', text: 'The demo was reset.', data: {}, at: iso(Date.now()) };
    this.log.push(ev);
    this.broadcast(ev);
  }

  broadcast(ev) {
    const msg = `id: ${ev.seq}\nevent: ${ev.type}\ndata: ${JSON.stringify(ev)}\n\n`;
    for (const res of this.clients) res.write(msg);
    if (ev.report_id) {
      for (const c of this.publicClients) {
        if (c.id !== ev.report_id) continue;
        const inst = this.find(c.id);
        if (inst) c.res.write(`event: update\ndata: ${JSON.stringify(publicReport(this.report(inst)))}\n\n`);
      }
    }
  }

  newReport(body) {
    const seq = Math.max(0, ...this.instances.map((i) => i.f.seq)) + 1;
    const id = 'r_' + Math.random().toString(36).slice(2, 12).padEnd(10, 'x');
    const busyUntil = Math.max(Date.now(), ...this.instances.filter((i) => i.live).map((i) => i.start + i.entries[i.entries.length - 1].t * 1000));
    const scn = {
      ...freeDelivery,
      text: body.text.trim(),
      reporter: { name: body.reporter?.name || '', email: body.reporter?.email || '' },
      context: { ...freeDelivery.context, ...(body.context || {}), breadcrumbs: body.context?.breadcrumbs || [], console_errors: body.context?.console_errors || [] },
    };
    const wait = Math.max(0, (busyUntil - Date.now()) / 1000 + 1);
    const f = { id, seq, scn, scale: 1, live: true, phases: [{ intake: true, wait }, { reproduce: 'failed' }, { attempt: 1, outcome: 'verified' }, { prove: true, testId: 'next' }] };
    const start = Date.now();
    const inst = this.instance(f, start, f.phases, skeleton(f, start), { live: true, useSuite: true });
    this.schedule(inst);
    return { id, seq };
  }

  system() {
    const now = Date.now();
    const list = this.reports(now, true);
    const active = list.find((r) => ['triaging', 'reproducing', 'fixing', 'verifying', 'regressing'].includes(r.status));
    const ready = { found: true, signed_in: 'yes', stub: false, problem: '', remedy: '' };
    const sim = this.name === 'simulated';
    const kane = { name: 'Kane CLI', path: '/usr/local/bin/kane-cli', version: '0.10.0', ...ready };
    const agent = { name: 'Claude Code', path: '/usr/local/bin/claude', version: '2.1.4', ...ready, signed_in: 'unknown' };
    if (sim) {
      Object.assign(kane, { path: '/home/you/fixpoint/fixpoint', version: 'stub-kane 0.0.0', stub: true });
      Object.assign(agent, { path: '/home/you/fixpoint/fixpoint', version: 'stub-agent 0.0.0', stub: true });
    }
    if (this.name === 'kane-missing') {
      Object.assign(kane, { found: false, path: '', version: '', signed_in: 'unknown', problem: 'Kane CLI was not found on this machine.', remedy: 'Install Kane CLI: `npm install -g @testmuai/kane-cli`, then `kane-cli login`.' });
    }
    if (this.name === 'signed-out') {
      Object.assign(kane, { signed_in: 'no', problem: 'Kane CLI is installed but not signed in.', remedy: 'Run `kane-cli login`, then reload this page.' });
    }
    return {
      version: '0.1.0-dev', simulated: sim, kane, agent,
      target: { name: 'Tapri', url: 'http://localhost:4000', dir: '/home/you/fixpoint/.fixpoint/shop', reachable: this.name !== 'target-down', demo: true },
      queue: { busy: !!active, active_report_id: active ? active.id : '', pending: list.filter((r) => r.status === 'queued').length },
      max_attempts: 3, autonomy: this.name === 'ask' ? 'ask' : 'auto', console_url: `http://localhost:${PORT}`,
      widget_snippet: `<script src="http://localhost:${PORT}/widget.js" data-fixpoint="http://localhost:${PORT}" defer></script>`,
    };
  }
}

const ACTIVE = ['queued', 'triaging', 'reproducing', 'fixing', 'verifying', 'regressing'];
const PERSON = ['needs_info', 'awaiting_approval', 'escalated', 'error'];

function summary(r) {
  return {
    id: r.id, seq: r.seq, status: r.status, title: r.title, text: r.text.length > 200 ? r.text.slice(0, 199) + '…' : r.text,
    attempts: r.attempts.length, max_attempts: r.max_attempts, time_to_proof_s: r.proof ? r.proof.time_to_proof_s : 0,
    queue_position: r.queue_position, simulated: r.simulated, track: r.track, created_at: r.created_at, updated_at: r.updated_at,
  };
}

function stats(list, suite) {
  const ttp = list.filter((r) => r.proof).map((r) => r.proof.time_to_proof_s).sort((a, b) => a - b);
  const mid = ttp.length ? (ttp.length % 2 ? ttp[(ttp.length - 1) / 2] : (ttp[ttp.length / 2 - 1] + ttp[ttp.length / 2]) / 2) : 0;
  return {
    reports: list.length, proven: list.filter((r) => r.status === 'proven').length,
    in_progress: list.filter((r) => ACTIVE.includes(r.status)).length,
    needs_you: list.filter((r) => PERSON.includes(r.status)).length,
    median_time_to_proof_s: Math.round(mid * 10) / 10, suite_size: suite.length,
    credits_used: Math.round(list.flatMap((r) => r.runs).reduce((s, x) => s + x.credits, 0) * 10) / 10,
    agent_cost_usd: Math.round(list.flatMap((r) => r.attempts).reduce((s, a) => s + a.agent.cost_usd, 0) * 100) / 100,
  };
}

function publicReport(r) {
  return {
    id: r.id, seq: r.seq, text: r.text, thread: r.thread, view: r.public,
    done: ['proven', 'unreproduced', 'escalated'].includes(r.status), simulated: r.simulated,
    created_at: r.created_at, updated_at: r.updated_at,
  };
}

const worlds = new Map();
function world(name, freeze) {
  const key = `${name}|${freeze ?? ''}`;
  if (!worlds.has(key)) worlds.set(key, new World(name, freeze));
  return worlds.get(key);
}

const MIME = {
  '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8', '.css': 'text/css; charset=utf-8',
  '.woff2': 'font/woff2', '.svg': 'image/svg+xml', '.png': 'image/png', '.txt': 'text/plain; charset=utf-8', '.json': 'application/json',
};

function cookies(req) {
  const out = {};
  for (const part of (req.headers.cookie || '').split(';')) {
    const [k, ...v] = part.trim().split('=');
    if (k) out[k] = decodeURIComponent(v.join('='));
  }
  return out;
}

function send(res, code, body, headers = {}) {
  res.writeHead(code, { 'Cache-Control': 'no-cache', ...headers });
  res.end(body);
}
const json = (res, code, obj, headers) => send(res, code, JSON.stringify(obj), { 'Content-Type': 'application/json; charset=utf-8', ...headers });
const fail = (res, code, error, headers) => json(res, code, { error }, headers);

function readBody(req) {
  return new Promise((resolve) => {
    let data = '';
    req.on('data', (c) => { data += c; if (data.length > 65536) req.destroy(); });
    req.on('end', () => { try { resolve(data ? JSON.parse(data) : {}); } catch { resolve(null); } });
  });
}

const PUBLIC_CORS = { 'Access-Control-Allow-Origin': '*', 'Access-Control-Allow-Headers': 'Content-Type', 'Access-Control-Allow-Methods': 'GET, POST, OPTIONS' };

function serveFile(res, file, extraHeaders = {}) {
  fs.readFile(file, (err, buf) => {
    if (err) return send(res, 404, 'Not found', { 'Content-Type': 'text/plain' });
    send(res, 200, buf, { 'Content-Type': MIME[path.extname(file)] || 'application/octet-stream', ...extraHeaders });
  });
}

const shotCache = new Map();

function mockIndex() {
  const live = ['/', `/reports/${LIVE_ID}`];
  const ids = fixtures().map((f) => [f.seq, f.id, f.scn.title]);
  const li = (href, text) => `<li><a href="${href}">${text}</a></li>`;
  return `<!doctype html><meta charset="utf-8"><title>Fixpoint mock</title>
<style>body{font:15px/1.5 system-ui;margin:40px;max-width:760px;color:#132238}a{color:#2743D2}code{font-size:13px}</style>
<h1>Fixpoint mock</h1>
<p>Each link sets the world in a cookie. <code>?freeze=18</code> holds the live report 18 seconds in; <code>?restart</code> replays it.</p>
<h2>Worlds</h2><ul>${Object.entries(SCENARIOS).map(([k, v]) => li(`/?scenario=${k}`, `${k}: ${v}`)).join('')}</ul>
<h2>Live report</h2><ul>${li(`/?scenario=default&restart`, 'Board, live from the start')}${[5, 10, 18, 25, 32, 45].map((s) => li(`/reports/${LIVE_ID}?scenario=default&freeze=${s}`, `Case file frozen at ${s}s`)).join('')}</ul>
<h2>Case files</h2><ul>${ids.map(([n, id, t]) => li(`/reports/${id}?scenario=default`, `#${n} ${t}`)).join('')}</ul>
<h2>Status pages</h2><ul>${ids.map(([n, id, t]) => li(`/r/${id}?scenario=default`, `#${n} ${t}`)).join('')}</ul>
<h2>Other views</h2><ul>${li('/suite?scenario=default', 'Suite')}${li('/setup?scenario=default', 'Setup')}${li('/setup?scenario=kane-missing', 'Setup with Kane missing')}${li('/reports/r_nope?scenario=default', 'A report that does not exist')}</ul>`;
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, `http://${req.headers.host || 'localhost'}`);
  const p = url.pathname;
  const jar = cookies(req);
  const qScenario = url.searchParams.get('scenario');
  const qFreeze = url.searchParams.get('freeze');
  const scenario = qScenario || jar.fxmock || DEFAULT_SCENARIO;
  const freezeRaw = qFreeze ?? (qScenario ? '' : jar.fxfreeze || '');
  const freeze = freezeRaw === '' || freezeRaw == null ? null : Number(freezeRaw);
  const w = world(SCENARIOS[scenario] ? scenario : 'default', Number.isFinite(freeze) ? freeze : null);

  if (p === '/__mock') return send(res, 200, mockIndex(), { 'Content-Type': 'text/html; charset=utf-8' });

  if (p.startsWith('/api/') || p.startsWith('/artifacts/')) {
    if (w.name === 'down') return fail(res, 503, 'The mock is pretending the engine is down.');
    if (w.name === 'slow' && req.method === 'GET' && p !== '/api/events') await new Promise((r) => setTimeout(r, 6000));
  }

  if (p.startsWith('/artifacts/')) {
    const spec = artifacts.get(p);
    if (!spec) return send(res, 404, 'Not found', { 'Content-Type': 'text/plain' });
    if (!shotCache.has(p)) shotCache.set(p, shot(spec));
    return send(res, 200, shotCache.get(p), { 'Content-Type': 'image/png' });
  }

  if (p.startsWith('/api/public/')) {
    if (req.method === 'OPTIONS') return send(res, 204, '', PUBLIC_CORS);
    if (p === '/api/public/reports' && req.method === 'POST') {
      const body = await readBody(req);
      const text = (body && typeof body.text === 'string' ? body.text : '').trim();
      if (text.length < 5) return fail(res, 400, 'Say a little more about what went wrong: at least 5 characters.', PUBLIC_CORS);
      const { id, seq } = w.newReport({ ...body, text });
      const origin = `http://${req.headers.host}`;
      return json(res, 201, { id, seq, status_url: `${origin}/r/${id}`, events_url: `${origin}/api/public/reports/${id}/events` }, PUBLIC_CORS);
    }
    const m = p.match(/^\/api\/public\/reports\/([\w-]+)(\/reply|\/events)?$/);
    const inst = m && w.find(m[1]);
    if (!inst) return fail(res, 404, 'There is no report with that address.', PUBLIC_CORS);
    if (!m[2] && req.method === 'GET') return json(res, 200, publicReport(w.report(inst)), PUBLIC_CORS);
    if (m[2] === '/reply' && req.method === 'POST') return reply(req, res, w, inst, PUBLIC_CORS);
    if (m[2] === '/events') {
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive', ...PUBLIC_CORS });
      res.write(`event: update\ndata: ${JSON.stringify(publicReport(w.report(inst)))}\n\n`);
      const c = { id: inst.f.id, res };
      w.publicClients.add(c);
      const ka = setInterval(() => res.write(': keep-alive\n\n'), 15000);
      req.on('close', () => { clearInterval(ka); w.publicClients.delete(c); });
      return;
    }
    return fail(res, 405, 'That method is not allowed here.', PUBLIC_CORS);
  }

  if (p.startsWith('/api/')) {
    if (req.method === 'POST') {
      if (req.headers['x-fixpoint'] !== '1') return fail(res, 403, 'This request needs the X-Fixpoint header. Reload the console and try again.');
      if (req.headers['sec-fetch-site'] === 'cross-site') return fail(res, 403, 'Requests from other sites are refused.');
    }
    if (p === '/api/system') return json(res, 200, w.system());
    if (p === '/api/reports') {
      const list = w.reports();
      return json(res, 200, { reports: list.map(summary), stats: stats(list, w.suite) });
    }
    if (p === '/api/suite') return json(res, 200, { tests: w.suite, running: w.suiteRunning });
    if (p === '/api/suite/run' && req.method === 'POST') {
      if (!w.suite.length) return fail(res, 409, 'The suite is empty. Each proven fix adds its check here.');
      if (!w.runSuite()) return fail(res, 409, 'The suite is already running.');
      return json(res, 202, {});
    }
    if (p === '/api/demo/reset' && req.method === 'POST') {
      const body = await readBody(req);
      if (!body || body.confirm !== 'reset') return fail(res, 400, 'Send {"confirm":"reset"} to reset the demo.');
      w.reset();
      return json(res, 200, {});
    }
    if (p === '/api/events') {
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive' });
      const since = Number(req.headers['last-event-id'] || url.searchParams.get('since') || 0);
      if (since) for (const ev of w.log) if (ev.seq > since) res.write(`id: ${ev.seq}\nevent: ${ev.type}\ndata: ${JSON.stringify(ev)}\n\n`);
      res.write(': connected\n\n');
      w.clients.add(res);
      const ka = setInterval(() => res.write(': keep-alive\n\n'), 15000);
      req.on('close', () => { clearInterval(ka); w.clients.delete(res); });
      return;
    }
    const m = p.match(/^\/api\/reports\/([\w-]+)(\/retry|\/approve|\/reply|\/patch)?$/);
    if (m) {
      const inst = w.find(m[1]);
      if (!inst) return fail(res, 404, 'There is no report with that id. It may have been cleared by a demo reset.');
      const r = w.report(inst);
      if (!m[2]) return json(res, 200, { report: r, events: w.log.filter((e) => e.report_id === r.id) });
      if (m[2] === '/patch') {
        const files = r.proof ? r.proof.files : (r.attempts[r.attempts.length - 1] || { files: [] }).files;
        if (!files.length) return fail(res, 404, 'This report has no patch yet.');
        return send(res, 200, files.map((f) => f.patch).join(''), {
          'Content-Type': 'text/x-diff; charset=utf-8', 'Content-Disposition': `attachment; filename="fixpoint-report-${r.seq}.patch"`,
        });
      }
      if (m[2] === '/retry' && req.method === 'POST') {
        if (!['error', 'escalated', 'unreproduced', 'needs_info'].includes(r.status)) return fail(res, 409, `A report that is ${r.status.replace('_', ' ')} cannot be retried.`);
        const phases = inst.f.retryPhases || [{ retry: true }, ...inst.f.phases.filter((x) => !x.intake)];
        w.continueWith(inst, phases, { scale: 1 });
        return json(res, 202, {});
      }
      if (m[2] === '/approve' && req.method === 'POST') {
        if (r.status !== 'awaiting_approval') return fail(res, 409, 'Only a report waiting for approval can be approved.');
        const phases = inst.f.approvePhases || [{ resume: true, redRun: r.runs.find((x) => x.kind === 'reproduce' && x.status === 'failed')?.id }, { attempt: 1, outcome: 'verified' }, { prove: true, testId: 'next' }];
        w.continueWith(inst, phases, { scale: 1, useSuite: true });
        return json(res, 202, {});
      }
      if (m[2] === '/reply' && req.method === 'POST') return reply(req, res, w, inst, {});
    }
    return fail(res, 404, 'There is no such API endpoint.');
  }

  // Static files and history routes.
  const headers = {};
  const isPage = !path.extname(p) || p.endsWith('.html');
  if (isPage && (qScenario || qFreeze != null || url.searchParams.has('restart'))) {
    headers['Set-Cookie'] = [`fxmock=${encodeURIComponent(scenario)}; Path=/; SameSite=Lax`, `fxfreeze=${qFreeze ?? ''}; Path=/; SameSite=Lax`];
  }
  if (url.searchParams.has('restart') && !w.freeze) {
    const live = fixtures().find((f) => f.live);
    const inst = w.instances.find((i) => i.f.id === live.id);
    w.startLive(inst ? inst.f : live, 400);
  }
  if (/^\/r\/[\w-]+$/.test(p)) return serveFile(res, path.join(ROOT, 'status.html'), headers);
  const rel = path.normalize(decodeURIComponent(p)).replace(/^(\.\.[/\\])+/, '');
  const file = path.join(ROOT, rel);
  if (!file.startsWith(ROOT)) return send(res, 403, 'Forbidden');
  if (path.extname(p) && fs.existsSync(file) && fs.statSync(file).isFile()) return serveFile(res, file, headers);
  if (path.extname(p)) return send(res, 404, 'Not found', { 'Content-Type': 'text/plain' });
  return serveFile(res, path.join(ROOT, 'index.html'), headers);
});

async function reply(req, res, w, inst, headers) {
  const body = await readBody(req);
  const text = (body && typeof body.text === 'string' ? body.text : '').trim();
  if (text.length < 2) return fail(res, 400, 'Write a reply before sending it.', headers);
  const r = w.report(inst);
  if (r.status !== 'needs_info') return fail(res, 409, 'This report is not waiting for an answer.', headers);
  const phases = inst.f.replyPhases ? inst.f.replyPhases(text) : [{ reply: text }, { reproduce: 'passed' }, { ask: inst.f.scn.followUp || inst.f.scn.question }];
  w.continueWith(inst, phases, { scale: 1 });
  return json(res, 202, {}, headers);
}

server.listen(PORT, '127.0.0.1', () => {
  console.log(`Fixpoint mock on http://localhost:${PORT}  (world: ${DEFAULT_SCENARIO})`);
  console.log(`Every state: http://localhost:${PORT}/__mock`);
});

export { fmtDuration };
