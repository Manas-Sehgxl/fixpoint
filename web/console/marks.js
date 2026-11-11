// Status marks are plot points, not pills: each state has its own shape so
// nothing depends on colour alone.
//   pending  hollow circle      done    filled ink circle
//   active   ringed plot blue   passed  filled green circle
//   failed   red cross          waiting amber diamond

import { s, h } from './dom.js';

const LABEL = {
  pending: 'Not started', done: 'Done', active: 'In progress', passed: 'Passed', failed: 'Failed', waiting: 'Waiting for a person',
};

export function markShape(state, c = 8) {
  switch (state) {
    case 'done': return [s('circle', { cx: c, cy: c, r: 4.5, class: 'm-ink' })];
    case 'passed': return [s('circle', { cx: c, cy: c, r: 4.5, class: 'm-pass' })];
    case 'active': return [s('circle', { cx: c, cy: c, r: 6.25, class: 'm-ring' }), s('circle', { cx: c, cy: c, r: 3, class: 'm-blue' })];
    case 'failed': return [s('path', { d: `M${c - 4} ${c - 4}L${c + 4} ${c + 4}M${c + 4} ${c - 4}L${c - 4} ${c + 4}`, class: 'm-cross' })];
    case 'waiting': return [s('path', { d: `M${c} ${c - 5.5}L${c + 5.5} ${c}L${c} ${c + 5.5}L${c - 5.5} ${c}Z`, class: 'm-wait' })];
    default: return [s('circle', { cx: c, cy: c, r: 4.25, class: 'm-hollow' })];
  }
}

/** mark draws one status mark. Pass label to make it speak; otherwise it is decorative. */
export function mark(state, label) {
  const attrs = { viewBox: '0 0 16 16', width: 16, height: 16, class: `mark mark-${state}` };
  if (label === undefined) attrs['aria-hidden'] = 'true';
  else { attrs.role = 'img'; attrs['aria-label'] = label || LABEL[state] || state; }
  return s('svg', attrs, markShape(state));
}

/** track draws the five-node track on a thin line. */
export function track(nodes, { labels = false } = {}) {
  const list = nodes && nodes.length ? nodes : [];
  const said = list.map((n) => `${n.label}: ${LABEL[n.state === 'done' ? 'done' : n.state] || n.state}`).join(', ');
  const w = 22 * (list.length - 1) + 16;
  const svg = s('svg', { viewBox: `0 0 ${w} 16`, width: w, height: 16, class: 'track-svg', 'aria-hidden': 'true' },
    s('line', { x1: 8, y1: 8, x2: w - 8, y2: 8, class: 'track-line' }),
    list.map((n, i) => s('g', { transform: `translate(${i * 22} 0)` }, markShape(trackState(n, i, list)))),
  );
  if (!labels) return h('span', { class: 'track', role: 'img', 'aria-label': `Track. ${said}.` }, svg);
  return h('ol', { class: 'track track-labelled', 'aria-label': 'Track' },
    list.map((n, i) => h('li', { class: `track-node is-${n.state}` }, mark(trackState(n, i, list)), h('span', null, n.label),
      h('span', { class: 'visually-hidden' }, `: ${LABEL[n.state] || n.state}`))));
}

function trackState(n, i, list) {
  if (n.state === 'done' && i === list.length - 1) return 'passed';
  return n.state;
}

/** logo is the cobweb mark, reduced: curve, diagonal, a short path and its fixed point. */
export function logo(size = 22) {
  const f = (x) => 0.42 + 0.5 * Math.sqrt(x);
  const S = 20, P = 2;
  const X = (x) => P + x * S, Y = (y) => P + (1 - y) * S;
  let d = '';
  for (let i = 0; i <= 24; i++) { const x = i / 24; d += `${i ? 'L' : 'M'}${X(x).toFixed(2)} ${Y(f(x)).toFixed(2)}`; }
  let x = 0.06, path = `M${X(x)} ${Y(0)}`;
  for (let k = 0; k < 3; k++) { const y = f(x); path += `L${X(x).toFixed(2)} ${Y(y).toFixed(2)}L${X(y).toFixed(2)} ${Y(y).toFixed(2)}`; x = y; }
  const xs = 0.8923;
  return s('svg', { viewBox: '0 0 24 24', width: size, height: size, class: 'logo', 'aria-hidden': 'true' },
    s('path', { d: `M${X(0)} ${Y(0)}L${X(1)} ${Y(1)}`, class: 'logo-diag' }),
    s('path', { d, class: 'logo-curve' }),
    s('path', { d: path, class: 'logo-path' }),
    s('circle', { cx: X(xs), cy: Y(xs), r: 2.2, class: 'logo-point' }),
  );
}
