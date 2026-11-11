// The before and after comparison: the failing and passing screenshots laid
// over each other with a divider you can drag, click, or move with the
// arrow keys (Shift for bigger steps, Home and End for the edges).

import { h } from './dom.js';

export function compare({ before, after, beforeLabel = 'Before the fix', afterLabel = 'After the fix', alt = '', start = 50, onMove }) {
  let pos = start;
  const afterImg = h('img', { src: after, alt: `${afterLabel}. ${alt}`.trim(), class: 'cmp-img', draggable: 'false' });
  const beforeImg = h('img', { src: before, alt: `${beforeLabel}. ${alt}`.trim(), class: 'cmp-img', draggable: 'false' });
  const beforeLayer = h('div', { class: 'cmp-before' }, beforeImg);
  const handle = h('div', {
    class: 'cmp-handle', role: 'slider', tabindex: '0', 'aria-label': 'Divider between before and after',
    'aria-valuemin': '0', 'aria-valuemax': '100', 'aria-orientation': 'horizontal',
  }, h('span', { class: 'cmp-grip', 'aria-hidden': 'true' }));
  const stage = h('div', { class: 'cmp-stage' }, afterImg, beforeLayer, handle);
  const el = h('figure', { class: 'cmp' },
    h('div', { class: 'cmp-labels', 'aria-hidden': 'true' },
      h('span', { class: 'cmp-label cmp-label-before' }, beforeLabel),
      h('span', { class: 'cmp-label cmp-label-after' }, afterLabel)),
    stage,
    h('figcaption', { class: 'cmp-hint' }, 'Drag the divider, or focus it and use the arrow keys, to compare the two screenshots.'));

  function set(p) {
    pos = Math.max(0, Math.min(100, p));
    beforeLayer.style.clipPath = `inset(0 ${100 - pos}% 0 0)`;
    handle.style.left = `${pos}%`;
    handle.setAttribute('aria-valuenow', String(Math.round(pos)));
    handle.setAttribute('aria-valuetext', pos <= 0 ? 'Showing only after' : pos >= 100 ? 'Showing only before' : `${Math.round(pos)}% before, ${Math.round(100 - pos)}% after`);
    if (onMove) onMove(pos);
  }
  set(pos);

  const fromEvent = (e) => {
    const rect = stage.getBoundingClientRect();
    return ((e.clientX - rect.left) / rect.width) * 100;
  };
  stage.addEventListener('pointerdown', (e) => {
    if (e.button !== 0) return;
    stage.setPointerCapture(e.pointerId);
    stage.classList.add('is-dragging');
    set(fromEvent(e));
    handle.focus({ preventScroll: true });
    e.preventDefault();
  });
  stage.addEventListener('pointermove', (e) => { if (stage.hasPointerCapture(e.pointerId)) set(fromEvent(e)); });
  const end = (e) => { if (stage.hasPointerCapture(e.pointerId)) stage.releasePointerCapture(e.pointerId); stage.classList.remove('is-dragging'); };
  stage.addEventListener('pointerup', end);
  stage.addEventListener('pointercancel', end);
  handle.addEventListener('keydown', (e) => {
    const step = e.shiftKey ? 10 : 2;
    const k = { ArrowLeft: -step, ArrowDown: -step, ArrowRight: step, ArrowUp: step, PageDown: -25, PageUp: 25 }[e.key];
    if (k !== undefined) { set(pos + k); e.preventDefault(); }
    else if (e.key === 'Home') { set(0); e.preventDefault(); }
    else if (e.key === 'End') { set(100); e.preventDefault(); }
  });
  return el;
}
