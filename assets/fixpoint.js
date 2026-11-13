// Fixpoint's page behaviour over the kit: the hero's one multiverse moment, the hero film,
// and the edge flicker on the main action. Plays once; reduced motion gets a plain fade.

const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
const win = document.querySelector('.hero__window');
const app = win && win.querySelector(':scope > .app');
const film = win && win.querySelector('[data-hero-film]');
const video = film && film.querySelector('video');
const play = film && film.querySelector('[data-film-play]');

// The same panel in four other universes, each a cause the bug might have had.
const UNIVERSES = [
  ['acid', 'what if: stale cache', -96, -46, -1.6],
  ['magenta', 'what if: race condition', 92, -40, 1.4],
  ['electric', 'what if: bad deploy', -84, 34, 1.1],
  ['sodium', 'what if: wrong timezone', 100, 30, -1.2],
];

function split() {
  if (!app || reduce) return Promise.resolve();
  // where the panel really sits in the frame (the kit centres it with a transform)
  const a = app.getBoundingClientRect(), w = win.getBoundingClientRect();
  const box = { left: a.left - w.left, top: a.top - w.top, width: a.width, height: a.height };
  // offsets are for a desktop panel; a phone's narrower panel moves less
  const k = Math.min(1, box.width / 880);
  const copies = UNIVERSES.map(([name, label, dx, dy, r], i) => {
    const wrap = document.createElement('div');
    wrap.className = `uv uv-${name}`;
    wrap.setAttribute('aria-hidden', 'true');
    wrap.inert = true;
    Object.assign(wrap.style, { left: `${box.left}px`, top: `${box.top}px`, width: `${box.width}px`, height: `${box.height}px` });
    wrap.style.setProperty('--dx', `${dx * k}px`); wrap.style.setProperty('--dy', `${dy * k}px`);
    wrap.style.setProperty('--r', `${r}deg`); wrap.style.setProperty('--d', `${i * 40}ms`);
    const copy = app.cloneNode(true);
    copy.removeAttribute('role'); copy.removeAttribute('aria-label');
    copy.querySelectorAll('[id]').forEach((el) => el.removeAttribute('id'));
    const tag = document.createElement('span');
    tag.className = 'uv__label';
    tag.textContent = label;
    wrap.append(copy, tag);
    win.insertBefore(wrap, app);
    return wrap;
  });
  app.classList.add('is-splitting');
  return new Promise((done) => setTimeout(() => {
    copies.forEach((c) => c.remove());
    app.classList.remove('is-splitting');
    settle();
    done();
  }, 1250));
}

// Back in this universe, the report being worked on is the one that holds: its row turns proven.
function settle() {
  const panel = app.querySelector('[data-ui="window"]');
  const st = app.querySelector('.ui-win__st.is-work');
  if (!panel || !st) return;
  st.textContent = panel.dataset.done || 'Proven';
  st.className = 'ui-win__st is-done';
  const row = st.closest('[data-row]');
  row.classList.remove('is-sel');
  row.classList.add('fp-just');
  setTimeout(() => row.classList.remove('fp-just'), 1400);
}

function startFilm() {
  if (!video || reduce || win.classList.contains('is-playing')) return;
  video.play().then(() => { win.classList.add('is-film'); app.inert = true; }, () => {});
}

function playWhole() {
  win.classList.add('is-film', 'is-playing');
  app.inert = true;
  video.removeAttribute('aria-hidden');
  video.removeAttribute('tabindex');
  video.loop = false;
  video.muted = false;
  video.controls = true;
  video.poster = 'assets/video/demo.jpg';
  video.src = 'assets/video/demo.mp4';
  video.setAttribute('aria-label', 'The Fixpoint demo: one real run, from the report to the proof');
  video.play().catch(() => {});
  video.focus();
}

if (win && app) {
  play && play.addEventListener('click', playWhole);
  const begin = () => split().then(() => setTimeout(startFilm, 2600));
  // after the page's own entrance, and only when the hero is on screen
  setTimeout(() => (win.getBoundingClientRect().top < innerHeight ? begin() : null), 900);
}

// Hovering the main action: the edges flicker with the other universes, at most once a second and a half.
if (!reduce) {
  const edges = document.createElement('div');
  edges.className = 'uv-edges';
  edges.setAttribute('aria-hidden', 'true');
  document.body.append(edges);
  let last = 0;
  document.querySelectorAll('.btn--primary').forEach((b) => b.addEventListener('pointerenter', () => {
    const now = performance.now();
    if (now - last < 1500) return;
    last = now;
    const root = document.documentElement;
    root.classList.remove('uv-flicker'); void edges.offsetWidth; root.classList.add('uv-flicker');
    setTimeout(() => root.classList.remove('uv-flicker'), 750);
  }));
}

// Moving to another section: the nav's rim flashes once with the other universes.
const bar = document.querySelector('header.nav > .nav__bar');
const links = document.querySelector('.nav__links');
if (!reduce && bar && links) {
  let current = null;
  new MutationObserver(() => {
    const on = links.querySelector('.is-current');
    if (!on || on === current) { current = on; return; }
    current = on;
    bar.classList.remove('fp-jump'); void bar.offsetWidth; bar.classList.add('fp-jump');
    setTimeout(() => bar.classList.remove('fp-jump'), 560);
  }).observe(links, { subtree: true, attributes: true, attributeFilter: ['class'] });
}
