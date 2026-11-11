// Every request the console and the status page make goes through here, so
// the same files work against the mock and the real engine. Paths follow
// SPEC section 10; nothing here knows about the mock.

const EVENT_TYPES = [
  'report.created', 'report.status', 'check.written', 'run.started', 'run.step', 'run.finished',
  'attempt.started', 'attempt.patched', 'attempt.finished', 'report.question', 'report.reply',
  'report.proven', 'report.escalated', 'report.error', 'report.requeued',
  'suite.started', 'suite.test', 'suite.finished', 'system.changed',
];

export class ApiError extends Error {
  constructor(message, status) {
    super(message);
    this.status = status;
  }
}

const UNREACHABLE = 'Fixpoint is not answering. Check that `fixpoint up` is still running, then try again.';

async function request(method, path, body, { console = true } = {}) {
  const headers = { Accept: 'application/json' };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (method !== 'GET' && console) headers['X-Fixpoint'] = '1';
  let res;
  try {
    res = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body), credentials: 'same-origin' });
  } catch {
    throw new ApiError(UNREACHABLE, 0);
  }
  const type = res.headers.get('Content-Type') || '';
  const data = type.includes('json') ? await res.json().catch(() => null) : null;
  if (!res.ok) {
    const msg = data && data.error ? data.error : `Fixpoint answered ${res.status} to ${method} ${path}.`;
    throw new ApiError(msg, res.status);
  }
  return data;
}

const enc = encodeURIComponent;

export const api = {
  system: () => request('GET', '/api/system'),
  reports: () => request('GET', '/api/reports'),
  report: (id) => request('GET', `/api/reports/${enc(id)}`),
  retry: (id) => request('POST', `/api/reports/${enc(id)}/retry`, {}),
  approve: (id) => request('POST', `/api/reports/${enc(id)}/approve`, {}),
  reply: (id, text) => request('POST', `/api/reports/${enc(id)}/reply`, { text }),
  patchURL: (id) => `/api/reports/${enc(id)}/patch`,
  suite: () => request('GET', '/api/suite'),
  runSuite: () => request('POST', '/api/suite/run', {}),
  resetDemo: () => request('POST', '/api/demo/reset', { confirm: 'reset' }),
  statusURL: (id) => `${location.origin}/r/${enc(id)}`,

  publicReport: (id) => request('GET', `/api/public/reports/${enc(id)}`, undefined, { console: false }),
  publicReply: (id, text) => request('POST', `/api/public/reports/${enc(id)}/reply`, { text }, { console: false }),
};

/**
 * stream keeps one EventSource open on url, reconnecting by itself. It calls
 * onState('open' | 'down') when the connection changes, waiting a moment
 * before reporting 'down' so a quick blip does not flash a notice, and
 * onResync after coming back from a real outage so callers can re-read.
 */
function stream(makeURL, types, { onMessage, onState, onResync }) {
  let es = null;
  let closed = false;
  let downTimer = null;
  let retryTimer = null;
  let wasDown = false;
  let backoff = 1000;

  const markDown = () => {
    if (downTimer || wasDown) return;
    downTimer = setTimeout(() => { wasDown = true; onState && onState('down'); }, 1500);
  };

  const open = () => {
    if (closed) return;
    es = new EventSource(makeURL());
    es.onopen = () => {
      backoff = 1000;
      clearTimeout(downTimer);
      downTimer = null;
      if (wasDown) {
        wasDown = false;
        onState && onState('open');
        onResync && onResync();
      }
    };
    es.onerror = () => {
      markDown();
      if (es.readyState === EventSource.CLOSED) {
        es = null;
        clearTimeout(retryTimer);
        retryTimer = setTimeout(open, backoff);
        backoff = Math.min(backoff * 2, 10000);
      }
    };
    for (const t of types) {
      es.addEventListener(t, (m) => {
        let data;
        try { data = JSON.parse(m.data); } catch { return; }
        onMessage(t, data, m.lastEventId);
      });
    }
  };
  open();
  return {
    close() {
      closed = true;
      clearTimeout(downTimer);
      clearTimeout(retryTimer);
      if (es) es.close();
    },
  };
}

/** events follows /api/events, resuming from the last sequence number seen. */
export function events({ onEvent, onState, onResync }) {
  let last = 0;
  return stream(() => (last ? `/api/events?since=${last}` : '/api/events'), EVENT_TYPES, {
    onMessage: (_t, ev) => {
      if (typeof ev.seq === 'number' && ev.seq > last) last = ev.seq;
      onEvent(ev);
    },
    onState,
    onResync,
  });
}

/** publicEvents follows one report's public stream; each message is a PublicReport. */
export function publicEvents(id, { onUpdate, onState, onResync }) {
  return stream(() => `/api/public/reports/${enc(id)}/events`, ['update'], {
    onMessage: (_t, report) => onUpdate(report),
    onState,
    onResync,
  });
}
