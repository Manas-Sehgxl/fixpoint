// The cobweb plot. Fixed-point iteration drawn on the unit square: the curve
// y = f(x), the diagonal y = x, and a path that steps between them. Every
// Kane run is a vertical move to the curve (red failed, green passed), every
// patch a horizontal move to the diagonal (plot blue). A proven report closes
// on the fixed point. The plot is rebuilt from the report each time; only
// moves it has not drawn before are animated.

import { s, h, reducedMotion } from './dom.js';

const f = (x) => 0.42 + 0.5 * Math.sqrt(x);
const X0 = 0.06;
const XSTAR = (() => { const r = (0.5 + Math.sqrt(0.25 + 4 * 0.42)) / 2; return r * r; })();

const SIZE = 300, PL = 30, PT = 12, PR = 12, PB = 28;
const W = PL + SIZE + PR, H = PT + SIZE + PB;
const X = (x) => PL + x * SIZE;
const Y = (y) => PT + (1 - y) * SIZE;

const ATTEMPT_WORDS = {
  verified: 'held', still_failing: 'did not hold', regressed: 'broke an earlier fix', no_change: 'changed nothing',
  revised_check: 'corrected the check', gave_up: 'gave up', agent_error: 'could not run', running: 'is under way',
};

/** model turns a report into moves and an end point. Exported for tests and the caption. */
export function model(r) {
  const moves = [];
  let x = X0, y = 0;
  let end = null;
  const story = [];
  const runs = r.runs || [];
  const repro = runs.filter((run) => run.kind === 'reproduce');
  const last = repro[repro.length - 1];
  const kane = (key, run, x0, y0, y1) => {
    const tone = run.status === 'failed' ? 'fail' : run.status === 'passed' ? 'pass' : null;
    if (tone) moves.push({ key: `${key}:${tone}`, kind: 'kane', tone, from: [x0, y0], to: [x0, y1], tip: tone === 'fail' ? 'cross' : 'dot' });
    return tone;
  };

  if (!last) {
    if (r.status === 'queued') end = { type: 'pending', at: [x, y] };
    else if (r.status === 'triaging') end = { type: 'active', at: [x, y] };
    story.push(r.status === 'queued' ? 'Waiting in line' : 'The check is being written');
  } else if (last.status === 'running') {
    end = { type: 'active', at: [x, y], preview: [x, f(x)] };
    story.push('Kane is trying to reproduce it');
  } else if (last.status === 'error' || last.status === 'timeout') {
    end = { type: 'failed', at: [x, y] };
    story.push(last.status === 'timeout' ? 'Kane ran out of time' : 'Kane could not start');
  } else {
    const tone = kane('repro', last, x, 0, f(x));
    y = f(x);
    story.push(tone === 'fail' ? 'Kane reproduced the bug' : repro.length > 1 ? `Kane could not reproduce it in ${repro.length} runs` : 'Kane could not reproduce it');
  }

  for (const a of r.attempts || []) {
    if (end) break;
    if (a.files && a.files.length) {
      moves.push({ key: `patch:${a.n}`, kind: 'patch', tone: 'patch', from: [x, y], to: [y, y] });
      x = y;
    } else if (a.status === 'running') {
      end = { type: 'active', at: [x, y], preview: [y, y] };
      story.push(`attempt ${a.n} is being written`);
      break;
    } else if (a.status === 'gave_up' || a.status === 'agent_error') {
      story.push(`attempt ${a.n} ${ATTEMPT_WORDS[a.status]}`);
      continue;
    } else {
      story.push(`attempt ${a.n} ${ATTEMPT_WORDS[a.status] || a.status}`);
      continue;
    }
    const v = runs.find((run) => run.id === a.verify_run_id) || runs.find((run) => run.kind === 'verify' && run.attempt === a.n);
    if (!v || v.status === 'running') {
      end = { type: 'active', at: [x, x], preview: [x, f(x)] };
      story.push(`attempt ${a.n} is being verified`);
      break;
    }
    if (v.status === 'error' || v.status === 'timeout') {
      end = { type: 'failed', at: [x, x] };
      story.push(`Kane could not verify attempt ${a.n}`);
      break;
    }
    kane(`verify:${a.n}`, v, x, x, f(x));
    y = f(x);
    const regs = runs.filter((run) => run.kind === 'regression' && run.attempt === a.n);
    if (regs.some((run) => run.status === 'failed')) moves[moves.length - 1].tip = 'cross-regressed';
    if (a.status === 'running' && v.status === 'passed') {
      end = { type: 'active', at: [x, y] };
      story.push(`attempt ${a.n} passed and earlier fixes are being re-checked`);
      break;
    }
    story.push(`attempt ${a.n} ${ATTEMPT_WORDS[a.status] || a.status}`);
  }

  let tail = [];
  if (!end) {
    switch (r.status) {
      case 'proven': {
        let tx = x, ty = y, guard = 0;
        while (Math.abs(ty - XSTAR) > 0.004 && guard++ < 12) {
          tail.push({ from: [tx, ty], to: [ty, ty] });
          tx = ty;
          tail.push({ from: [tx, tx], to: [tx, f(tx)] });
          ty = f(tx);
        }
        tail.push({ from: [tx, ty], to: [XSTAR, XSTAR] });
        tail = tail.map((t, i) => ({ ...t, key: `tail:${i}`, kind: 'tail', tone: 'tail' }));
        end = { type: 'fixed', at: [XSTAR, XSTAR] };
        story.push('the fix is proven');
        break;
      }
      case 'needs_info': end = { type: 'waiting', at: [x, y] }; story.push('waiting for the reporter'); break;
      case 'awaiting_approval': end = { type: 'waiting', at: [x, y] }; story.push('waiting for approval to fix it'); break;
      case 'escalated': end = { type: 'failed', at: [x, y] }; story.push('handed to a person'); break;
      case 'error': end = { type: 'failed', at: [x, y] }; story.push('stopped by an error'); break;
      case 'unreproduced': end = { type: 'closed', at: [x, y] }; story.push('closed'); break;
      default: end = { type: 'active', at: [x, y], preview: r.status === 'fixing' ? [y, y] : [x, f(x)] };
    }
  }
  const lastMove = moves[moves.length - 1];
  if (end && end.type === 'failed' && lastMove && lastMove.to[0] === end.at[0] && lastMove.to[1] === end.at[1]) delete lastMove.tip;
  const sentence = story.length ? story.join('; ').replace(/^./, (c) => c.toUpperCase()) + '.' : '';
  return { moves: moves.concat(tail), end, sentence };
}

const len = (m) => Math.hypot((m.to[0] - m.from[0]) * SIZE, (m.to[1] - m.from[1]) * SIZE);

function staticLayer(compact) {
  const g = s('g', { class: 'cw-static' });
  for (let i = 1; i < 10; i++) {
    g.append(s('line', { x1: X(i / 10), y1: Y(0), x2: X(i / 10), y2: Y(1), class: 'cw-grid' }));
    g.append(s('line', { x1: X(0), y1: Y(i / 10), x2: X(1), y2: Y(i / 10), class: 'cw-grid' }));
  }
  g.append(s('rect', { x: X(0), y: Y(1), width: SIZE, height: SIZE, class: 'cw-frame' }));
  g.append(s('path', { d: `M${X(0)} ${Y(0)}H${X(1)}M${X(0)} ${Y(0)}V${Y(1)}`, class: 'cw-axis' }));
  g.append(s('line', { x1: X(0), y1: Y(0), x2: X(1), y2: Y(1), class: 'cw-diag' }));
  let d = '';
  for (let i = 0; i <= 120; i++) {
    const x = i / 120;
    d += `${i ? 'L' : 'M'}${X(x).toFixed(2)} ${Y(Math.min(1, f(x))).toFixed(2)}`;
  }
  g.append(s('path', { d, class: 'cw-curve' }));
  if (!compact) {
    g.append(s('text', { x: X(0.15), y: Y(0.69), class: 'cw-label', transform: `rotate(-27 ${X(0.15)} ${Y(0.69)})` }, 'y = f(x)'));
    g.append(s('text', { x: X(0.66), y: Y(0.57), class: 'cw-label', transform: `rotate(-45 ${X(0.66)} ${Y(0.57)})` }, 'y = x'));
    g.append(s('text', { x: X(1), y: Y(0) + 20, class: 'cw-label cw-axis-label', 'text-anchor': 'end' }, 'x'));
    g.append(s('text', { x: X(0) - 12, y: Y(1) + 10, class: 'cw-label cw-axis-label', 'text-anchor': 'middle' }, 'y'));
  }
  return g;
}

/**
 * createCobweb returns { el, update(report) }. Call update whenever the
 * report is re-read; new moves draw themselves in.
 */
export function createCobweb({ compact = false } = {}) {
  const movesG = s('g', { class: 'cw-moves' });
  const pointsG = s('g', { class: 'cw-points' });
  const ghost = s('circle', { cx: X(XSTAR), cy: Y(XSTAR), r: 5, class: 'cw-ghost' });
  const svg = s('svg', { viewBox: `0 0 ${W} ${H}`, class: compact ? 'cobweb cobweb-compact' : 'cobweb', role: 'img' },
    s('title', null, 'Cobweb plot of this report'), staticLayer(compact), ghost, movesG, pointsG);
  const caption = h('p', { class: 'cw-sentence' });
  const el = compact ? h('div', { class: 'cobweb-wrap' }, svg) : h('figure', { class: 'cobweb-figure' }, svg,
    h('figcaption', null, caption, legend()));
  const drawn = new Map();
  let first = true;
  let wasProven = false;
  let shownFor = null;

  function update(r) {
    if (shownFor !== r.id) { movesG.replaceChildren(); drawn.clear(); first = true; wasProven = false; shownFor = r.id; }
    const m = model(r);
    const animate = !first && !reducedMotion();
    const seen = new Set();
    let delay = 0;
    for (const mv of m.moves) {
      seen.add(mv.key);
      if (drawn.has(mv.key)) {
        const g = drawn.get(mv.key);
        if ((g.dataset.tip || '') !== (mv.tip || '')) {
          if (g.children[1]) g.children[1].remove();
          if (mv.tip) g.append(tip(mv));
          g.dataset.tip = mv.tip || '';
        }
        continue;
      }
      const g = s('g', { class: `cw-move mv-${mv.tone}` });
      const line = s('line', { x1: X(mv.from[0]), y1: Y(mv.from[1]), x2: X(mv.to[0]), y2: Y(mv.to[1]) });
      g.append(line);
      if (mv.tip) g.append(tip(mv));
      g.dataset.tip = mv.tip || '';
      movesG.append(g);
      drawn.set(mv.key, g);
      if (animate) {
        const L = len(mv);
        const dur = mv.kind === 'tail' ? 160 : 520;
        line.style.strokeDasharray = `${L}`;
        line.animate([{ strokeDashoffset: L }, { strokeDashoffset: 0 }], { duration: dur, delay, easing: 'cubic-bezier(.4,0,.2,1)', fill: 'backwards' })
          .finished.then(() => { line.style.strokeDasharray = ''; }).catch(() => {});
        if (g.children[1]) g.children[1].animate([{ opacity: 0 }, { opacity: 1 }], { duration: 180, delay: delay + dur, fill: 'backwards' });
        delay += mv.kind === 'tail' ? 130 : 420;
      }
    }
    for (const [k, g] of drawn) if (!seen.has(k)) { g.remove(); drawn.delete(k); }

    pointsG.replaceChildren();
    const start = s('circle', { cx: X(X0), cy: Y(0), r: 2.5, class: 'cw-start' });
    pointsG.append(start);
    if (!compact) pointsG.append(s('text', { x: X(X0) + 6, y: Y(0) + 18, class: 'cw-label cw-small' }, 'report'));
    const e = m.end;
    if (e) {
      const [ex, ey] = e.at;
      if (e.preview) pointsG.append(s('line', { x1: X(ex), y1: Y(ey), x2: X(e.preview[0]), y2: Y(e.preview[1]), class: 'cw-preview' }));
      const px = X(ex), py = Y(ey);
      if (e.type === 'fixed') {
        const dot = s('circle', { cx: px, cy: py, r: 6.5, class: 'cw-fixed' });
        const ring = s('circle', { cx: px, cy: py, r: 11, class: 'cw-fixed-ring' });
        pointsG.append(ring, dot);
        if (!compact) pointsG.append(s('text', { x: px - 14, y: py - 14, class: 'cw-label cw-fixed-label', 'text-anchor': 'end' }, 'f(x) = x'));
        if (animate && !wasProven) {
          const opts = { duration: 520, delay, easing: 'cubic-bezier(.2,.8,.2,1)', fill: 'backwards' };
          dot.animate([{ transform: 'scale(0)', opacity: 0 }, { transform: 'scale(1)', opacity: 1 }], opts);
          ring.animate([{ transform: 'scale(.4)', opacity: 0 }, { transform: 'scale(1)', opacity: 1 }], { ...opts, delay: delay + 140 });
          for (const t of pointsG.querySelectorAll('.cw-fixed-label')) t.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 400, delay: delay + 300, fill: 'backwards' });
        }
      } else if (e.type === 'active') {
        pointsG.append(s('circle', { cx: px, cy: py, r: 7, class: 'cw-active-ring' }), s('circle', { cx: px, cy: py, r: 3.2, class: 'cw-active' }));
      } else if (e.type === 'waiting') {
        pointsG.append(s('circle', { cx: px, cy: py, r: 6, class: 'cw-waiting' }));
      } else if (e.type === 'failed') {
        pointsG.append(s('path', { d: `M${px - 6} ${py - 6}L${px + 6} ${py + 6}M${px + 6} ${py - 6}L${px - 6} ${py + 6}`, class: 'cw-failed' }));
      } else if (e.type === 'closed') {
        pointsG.append(s('circle', { cx: px, cy: py, r: 5, class: 'cw-closed' }));
      } else {
        pointsG.append(s('circle', { cx: px, cy: py, r: 4.5, class: 'cw-pending' }));
      }
    }
    ghost.style.display = e && e.type === 'fixed' ? 'none' : '';
    svg.setAttribute('aria-label', `Cobweb plot. ${m.sentence}`);
    caption.textContent = m.sentence;
    wasProven = r.status === 'proven';
    first = false;
  }

  return { el, update };
}

function tip(mv) {
  const px = X(mv.to[0]), py = Y(mv.to[1]);
  if (mv.tip === 'dot') return s('circle', { cx: px, cy: py, r: 3, class: 'cw-tip' });
  const k = 4;
  return s('path', { d: `M${px - k} ${py - k}L${px + k} ${py + k}M${px + k} ${py - k}L${px - k} ${py + k}`, class: mv.tip === 'cross-regressed' ? 'cw-tip-cross cw-regressed' : 'cw-tip-cross' });
}

function legend() {
  const item = (cls, d, tipEl, text) => h('li', null,
    s('svg', { viewBox: '0 0 22 16', width: 22, height: 16, 'aria-hidden': 'true', class: `cw-move ${cls}` }, s('path', { d }), tipEl),
    text);
  return h('ul', { class: 'cw-legend', 'aria-label': 'How to read the plot' },
    item('mv-fail', 'M11 15V4', s('path', { d: 'M8 1L14 7M14 1L8 7', class: 'cw-tip-cross' }), 'Kane run that failed'),
    item('mv-pass', 'M11 15V4', s('circle', { cx: 11, cy: 4, r: 3, class: 'cw-tip' }), 'Kane run that passed'),
    item('mv-patch', 'M2 8H20', null, 'A patch'),
  );
}
