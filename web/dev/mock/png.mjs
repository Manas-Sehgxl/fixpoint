// Placeholder screenshots for the mock server, drawn in code and encoded as
// PNG with nothing but node:zlib. They imitate a page of the demo shop well
// enough that a before/after comparison reads at a glance.

import { deflateSync } from 'node:zlib';

const FONT = {
  'A': ['01110', '10001', '10001', '11111', '10001', '10001', '10001'],
  'B': ['11110', '10001', '10001', '11110', '10001', '10001', '11110'],
  'C': ['01111', '10000', '10000', '10000', '10000', '10000', '01111'],
  'D': ['11110', '10001', '10001', '10001', '10001', '10001', '11110'],
  'E': ['11111', '10000', '10000', '11110', '10000', '10000', '11111'],
  'F': ['11111', '10000', '10000', '11110', '10000', '10000', '10000'],
  'G': ['01111', '10000', '10000', '10011', '10001', '10001', '01111'],
  'H': ['10001', '10001', '10001', '11111', '10001', '10001', '10001'],
  'I': ['11111', '00100', '00100', '00100', '00100', '00100', '11111'],
  'J': ['00111', '00010', '00010', '00010', '00010', '10010', '01100'],
  'K': ['10001', '10010', '10100', '11000', '10100', '10010', '10001'],
  'L': ['10000', '10000', '10000', '10000', '10000', '10000', '11111'],
  'M': ['10001', '11011', '10101', '10101', '10001', '10001', '10001'],
  'N': ['10001', '11001', '10101', '10011', '10001', '10001', '10001'],
  'O': ['01110', '10001', '10001', '10001', '10001', '10001', '01110'],
  'P': ['11110', '10001', '10001', '11110', '10000', '10000', '10000'],
  'Q': ['01110', '10001', '10001', '10001', '10101', '10010', '01101'],
  'R': ['11110', '10001', '10001', '11110', '10100', '10010', '10001'],
  'S': ['01111', '10000', '10000', '01110', '00001', '00001', '11110'],
  'T': ['11111', '00100', '00100', '00100', '00100', '00100', '00100'],
  'U': ['10001', '10001', '10001', '10001', '10001', '10001', '01110'],
  'V': ['10001', '10001', '10001', '10001', '10001', '01010', '00100'],
  'W': ['10001', '10001', '10001', '10101', '10101', '10101', '01010'],
  'X': ['10001', '10001', '01010', '00100', '01010', '10001', '10001'],
  'Y': ['10001', '10001', '01010', '00100', '00100', '00100', '00100'],
  'Z': ['11111', '00001', '00010', '00100', '01000', '10000', '11111'],
  '0': ['01110', '10001', '10011', '10101', '11001', '10001', '01110'],
  '1': ['00100', '01100', '00100', '00100', '00100', '00100', '01110'],
  '2': ['01110', '10001', '00001', '00010', '00100', '01000', '11111'],
  '3': ['11110', '00001', '00001', '01110', '00001', '00001', '11110'],
  '4': ['00010', '00110', '01010', '10010', '11111', '00010', '00010'],
  '5': ['11111', '10000', '11110', '00001', '00001', '10001', '01110'],
  '6': ['00110', '01000', '10000', '11110', '10001', '10001', '01110'],
  '7': ['11111', '00001', '00010', '00100', '01000', '01000', '01000'],
  '8': ['01110', '10001', '10001', '01110', '10001', '10001', '01110'],
  '9': ['01110', '10001', '10001', '01111', '00001', '00010', '01100'],
  '₹': ['11111', '00010', '11111', '00010', '11100', '00110', '00011'],
  '.': ['00000', '00000', '00000', '00000', '00000', '01100', '01100'],
  ':': ['00000', '01100', '01100', '00000', '01100', '01100', '00000'],
  '-': ['00000', '00000', '00000', '01110', '00000', '00000', '00000'],
  '+': ['00000', '00100', '00100', '11111', '00100', '00100', '00000'],
  '%': ['11001', '11010', '00010', '00100', '01000', '01011', '10011'],
  '/': ['00001', '00010', '00010', '00100', '01000', '01000', '10000'],
  '"': ['01010', '01010', '00000', '00000', '00000', '00000', '00000'],
  ' ': ['00000', '00000', '00000', '00000', '00000', '00000', '00000'],
};

const CRC = new Uint32Array(256).map((_, n) => {
  let c = n;
  for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c >>> 0;
});

function crc32(buf) {
  let c = 0xffffffff;
  for (const b of buf) c = CRC[(c ^ b) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}

function chunk(type, data) {
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length);
  const body = Buffer.concat([Buffer.from(type, 'ascii'), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(body));
  return Buffer.concat([len, body, crc]);
}

class Canvas {
  constructor(w, h, bg) {
    this.w = w;
    this.h = h;
    this.px = Buffer.alloc(w * h * 3);
    this.rect(0, 0, w, h, bg);
  }
  rect(x, y, w, h, [r, g, b]) {
    const x0 = Math.max(0, x), y0 = Math.max(0, y);
    const x1 = Math.min(this.w, x + w), y1 = Math.min(this.h, y + h);
    for (let j = y0; j < y1; j++) {
      for (let i = x0; i < x1; i++) {
        const o = (j * this.w + i) * 3;
        this.px[o] = r; this.px[o + 1] = g; this.px[o + 2] = b;
      }
    }
  }
  frame(x, y, w, h, t, c) {
    this.rect(x, y, w, t, c);
    this.rect(x, y + h - t, w, t, c);
    this.rect(x, y, t, h, c);
    this.rect(x + w - t, y, t, h, c);
  }
  disc(cx, cy, r, c) {
    for (let j = -r; j <= r; j++) {
      const half = Math.floor(Math.sqrt(r * r - j * j));
      this.rect(cx - half, cy + j, half * 2 + 1, 1, c);
    }
  }
  text(x, y, s, scale, c) {
    let cx = x;
    for (const ch of s.toUpperCase()) {
      const g = FONT[ch] || FONT[' '];
      g.forEach((row, j) => {
        for (let i = 0; i < 5; i++) if (row[i] === '1') this.rect(cx + i * scale, y + j * scale, scale, scale, c);
      });
      cx += 6 * scale;
    }
    return cx;
  }
  textWidth(s, scale) { return [...s].length * 6 * scale - scale; }
  png() {
    const raw = Buffer.alloc((this.w * 3 + 1) * this.h);
    for (let j = 0; j < this.h; j++) {
      raw[j * (this.w * 3 + 1)] = 0;
      this.px.copy(raw, j * (this.w * 3 + 1) + 1, j * this.w * 3, (j + 1) * this.w * 3);
    }
    const ihdr = Buffer.alloc(13);
    ihdr.writeUInt32BE(this.w, 0);
    ihdr.writeUInt32BE(this.h, 4);
    ihdr[8] = 8; ihdr[9] = 2; ihdr[10] = 0; ihdr[11] = 0; ihdr[12] = 0;
    return Buffer.concat([
      Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
      chunk('IHDR', ihdr),
      chunk('IDAT', deflateSync(raw, { level: 9 })),
      chunk('IEND', Buffer.alloc(0)),
    ]);
  }
}

const STALL = [24, 74, 58];
const SIGN = [246, 196, 66];
const PAPER = [252, 247, 238];
const LINE = [226, 216, 200];
const INK = [40, 36, 32];
const SOFT = [120, 110, 98];
const RED = [200, 50, 31];
const GREEN = [14, 122, 75];
const GLASSES = [[176, 98, 46], [196, 132, 60], [150, 72, 40], [214, 164, 90], [120, 140, 60], [200, 110, 120]];

/**
 * shot draws one placeholder page capture.
 * spec: { step, of, cart: [[label, value]], mark: index into cart or -1,
 *         tone: 'fail' | 'pass' | 'plain', note: string, search: string }
 */
export function shot(spec) {
  const W = 960, H = 600;
  const c = new Canvas(W, H, PAPER);
  c.rect(0, 0, W, 72, STALL);
  c.text(32, 22, 'TAPRI', 4, SIGN);
  c.text(200, 31, 'CHAI STALL', 2, [210, 226, 214]);
  const step = `STEP ${spec.step}${spec.of ? ' OF ' + spec.of : ''}`;
  c.text(W - 32 - c.textWidth(step, 2), 31, step, 2, [210, 226, 214]);

  c.rect(32, 96, 560, 40, [255, 255, 255]);
  c.frame(32, 96, 560, 40, 2, LINE);
  c.text(46, 109, spec.search || 'SEARCH TEAS', 2, spec.search ? INK : SOFT);

  const names = ['MASALA CHAI', 'ELAICHI CHAI', 'KADAK ASSAM', 'KASHMIRI KAHWA', 'ADRAK CHAI', 'TULSI GREEN'];
  const prices = ['₹249', '₹199', '₹250', '₹320', '₹210', '₹230'];
  const shown = spec.empty ? [] : names.map((_, i) => i);
  shown.forEach((i) => {
    const col = i % 2, row = Math.floor(i / 2);
    const x = 32 + col * 284, y = 156 + row * 136;
    c.rect(x, y, 268, 120, [255, 255, 255]);
    c.frame(x, y, 268, 120, 2, LINE);
    c.rect(x + 20, y + 30, 44, 60, [236, 232, 224]);
    c.rect(x + 24, y + 44, 36, 42, GLASSES[i]);
    c.text(x + 82, y + 30, names[i], 2, INK);
    c.text(x + 82, y + 56, prices[i], 3, INK);
    c.rect(x + 82, y + 86, 96, 22, STALL);
    c.text(x + 90, y + 93, 'ADD TO CART', 1, [255, 255, 255]);
  });
  if (spec.empty) c.text(46, 200, 'NO TEAS FOUND', 3, SOFT);

  const px = 624, pw = W - px - 32;
  c.rect(px, 96, pw, 440, [255, 255, 255]);
  c.frame(px, 96, pw, 440, 2, LINE);
  c.text(px + 20, 116, 'YOUR CART', 3, INK);
  let y = 168;
  for (const [label, value] of spec.items || []) {
    c.disc(px + 28, y + 6, 6, GLASSES[0]);
    c.text(px + 44, y, label, 2, INK);
    c.text(px + pw - 20 - c.textWidth(value, 2), y, value, 2, INK);
    y += 30;
  }
  y = Math.max(y + 10, 280);
  c.rect(px + 20, y, pw - 40, 2, LINE);
  y += 20;
  (spec.cart || []).forEach(([label, value], i) => {
    const big = label === 'TOTAL';
    const s = big ? 3 : 2;
    c.text(px + 20, y, label, s, INK);
    c.text(px + pw - 20 - c.textWidth(value, s), y, value, s, INK);
    if (i === spec.mark) {
      const col = spec.tone === 'pass' ? GREEN : RED;
      c.frame(px + 10, y - 10, pw - 20, s * 7 + 20, 3, col);
    }
    y += big ? 44 : 34;
  });
  if (spec.note) {
    const col = spec.tone === 'pass' ? GREEN : spec.tone === 'fail' ? RED : SOFT;
    c.text(32, H - 40, spec.note, 2, col);
  }
  return c.png();
}
