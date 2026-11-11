// Small DOM helpers. Everything that came from a report goes through text
// nodes or attributes set here; nothing in the console uses innerHTML.

const SVG_NS = 'http://www.w3.org/2000/svg';

function apply(el, attrs) {
  if (!attrs) return;
  for (const [k, v] of Object.entries(attrs)) {
    if (v == null || v === false) continue;
    if (k === 'class') el.setAttribute('class', Array.isArray(v) ? v.filter(Boolean).join(' ') : v);
    else if (k === 'text') el.textContent = v;
    else if (k === 'on') for (const [ev, fn] of Object.entries(v)) el.addEventListener(ev, fn);
    else if (k === 'dataset') Object.assign(el.dataset, v);
    else if (k === 'style' && typeof v === 'object') Object.assign(el.style, v);
    else if (k in el && typeof v !== 'string' && el.namespaceURI !== SVG_NS) el[k] = v;
    else el.setAttribute(k, v === true ? '' : String(v));
  }
}

function append(el, children) {
  for (const c of children.flat(Infinity)) {
    if (c == null || c === false || c === '') continue;
    el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
}

/** h builds an HTML element. Strings become text nodes, never markup. */
export function h(tag, attrs, ...children) {
  const el = document.createElement(tag);
  apply(el, attrs);
  append(el, children);
  return el;
}

/** s builds an SVG element. */
export function s(tag, attrs, ...children) {
  const el = document.createElementNS(SVG_NS, tag);
  apply(el, attrs);
  append(el, children);
  return el;
}

export function clear(el) {
  while (el.firstChild) el.firstChild.remove();
  return el;
}

/**
 * rich renders plain text in which `backticks` mark commands, as remedies
 * from the engine are written. The text stays text: only <code> is added.
 */
export function rich(text) {
  const frag = document.createDocumentFragment();
  String(text || '').split('`').forEach((part, i) => {
    if (!part) return;
    frag.append(i % 2 ? h('code', null, part) : document.createTextNode(part));
  });
  return frag;
}

/**
 * reconcile makes parent's keyed children match items, in order. A child is
 * rebuilt only when its signature changes, so scroll position, focus and
 * open sections survive a re-read of the data.
 */
export function reconcile(parent, items, { key, sig, render }) {
  const old = new Map();
  for (const c of parent.children) if (c.dataset.key) old.set(c.dataset.key, c);
  const keep = new Set();
  items.forEach((item, i) => {
    const k = String(key(item));
    const sg = sig ? sig(item) : JSON.stringify(item);
    let node = old.get(k);
    if (!node || node._sig !== sg) {
      const fresh = render(item, node);
      if (node && fresh !== node) node.replaceWith(fresh);
      node = fresh;
      node.dataset.key = k;
      node._sig = sg;
    }
    keep.add(node);
    const at = parent.children[i];
    if (at !== node) parent.insertBefore(node, at || null);
  });
  for (const c of [...parent.children]) if (c.dataset.key && !keep.has(c)) c.remove();
}

/** announce speaks a short message through the shared live region. */
export function announce(text) {
  const region = document.getElementById('announcer');
  if (!region) return;
  region.textContent = '';
  setTimeout(() => { region.textContent = text; }, 30);
}

export async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const ta = h('textarea', { class: 'visually-hidden', readonly: true }, text);
    document.body.append(ta);
    ta.select();
    let ok = false;
    try { ok = document.execCommand('copy'); } catch { ok = false; }
    ta.remove();
    return ok;
  }
}

/** copyButton is a button that copies text and says so for two seconds. */
export function copyButton(label, getText, cls = 'btn') {
  const btn = h('button', { type: 'button', class: cls }, label);
  btn.addEventListener('click', async () => {
    const ok = await copyText(getText());
    btn.textContent = ok ? 'Copied' : 'Copy failed: select the text and copy it yourself';
    announce(ok ? 'Copied to the clipboard.' : 'Could not copy. Select the text and copy it yourself.');
    setTimeout(() => { btn.textContent = label; }, 2000);
  });
  return btn;
}

export const reducedMotion = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;
