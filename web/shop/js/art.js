// Product art: one small hand-drawn vessel per pouch, built as an SVG string
// from the colours in products.js. No images, no external requests.

function steam(x) {
  return `<path d="M${x} 30 c -5 -7 5 -11 0 -18 c -4 -6 3 -9 1 -13" fill="none" stroke="#15251D" stroke-width="2.4" stroke-linecap="round" opacity="0.45"/>`;
}

const VESSELS = {
  glass: ({ liquid, top }) => `
    <path d="M58 44 h44 l-6 52 h-32 z" fill="${liquid}" stroke="#15251D" stroke-width="3" stroke-linejoin="round"/>
    <path d="M58 44 h44 l-1.6 14 h-40.8 z" fill="${top}" stroke="none"/>
    <path d="M55 41 h50 v6 h-50 z" fill="#FFFBF2" stroke="#15251D" stroke-width="3" stroke-linejoin="round"/>`,
  kulhad: ({ liquid, top }) => `
    <path d="M56 46 h48 l-8 50 h-32 z" fill="#B5673A" stroke="#15251D" stroke-width="3" stroke-linejoin="round"/>
    <ellipse cx="80" cy="47" rx="24" ry="7" fill="${liquid}" stroke="#15251D" stroke-width="3"/>
    <ellipse cx="80" cy="46" rx="15" ry="4" fill="${top}" stroke="none"/>
    <path d="M60 68 h40" stroke="#15251D" stroke-width="2.4" opacity="0.35"/>`,
  cup: ({ liquid, top }) => `
    <path d="M52 48 h48 v26 a24 24 0 0 1 -48 0 z" fill="#FFFBF2" stroke="#15251D" stroke-width="3" stroke-linejoin="round"/>
    <path d="M56 52 h40 v20 a20 20 0 0 1 -40 0 z" fill="${liquid}" stroke="none"/>
    <ellipse cx="76" cy="53" rx="18" ry="5" fill="${top}" stroke="none"/>
    <path d="M100 54 a14 12 0 0 1 0 20" fill="none" stroke="#15251D" stroke-width="3"/>
    <path d="M40 96 h72" stroke="#15251D" stroke-width="3" stroke-linecap="round"/>`,
  tumbler: ({ liquid, top }) => `
    <path d="M40 78 h72 l-11 20 h-50 z" fill="#D8DEE2" stroke="#15251D" stroke-width="3" stroke-linejoin="round"/>
    <path d="M56 36 h40 l-5 46 h-30 z" fill="${liquid}" stroke="#15251D" stroke-width="3" stroke-linejoin="round"/>
    <path d="M56 36 h40 l-1.3 11 h-37.4 z" fill="${top}" stroke="none"/>
    <path d="M53 33 h46 v6 h-46 z" fill="#D8DEE2" stroke="#15251D" stroke-width="3" stroke-linejoin="round"/>`,
  tall: ({ liquid, top }) => `
    <path d="M60 32 h38 l-5 64 h-28 z" fill="${liquid}" stroke="#15251D" stroke-width="3" stroke-linejoin="round"/>
    <path d="M60 32 h38 l-1.3 10 h-35.4 z" fill="${top}" stroke="none"/>
    <circle cx="79" cy="60" r="9" fill="#9BD34F" stroke="#15251D" stroke-width="2.4"/>
    <path d="M70 60 h18 M79 51 v18" stroke="#15251D" stroke-width="1.6" opacity="0.6"/>
    <path d="M57 29 h44 v6 h-44 z" fill="#FFFBF2" stroke="#15251D" stroke-width="3" stroke-linejoin="round"/>`,
};

export function productArt(product) {
  const { vessel, panel } = product.art;
  const draw = VESSELS[vessel] ?? VESSELS.glass;
  return `<svg class="art" viewBox="0 0 160 110" role="img" aria-label="${product.name}, drawn as a glass of tea" focusable="false">
    <rect x="0" y="0" width="160" height="110" fill="${panel}"/>
    <path d="M0 96 h160" stroke="#15251D" stroke-width="2" opacity="0.25"/>
    ${steam(72)}${steam(92)}
    ${draw(product.art)}
  </svg>`;
}

export function cobwebMark(size = 18) {
  return `<svg width="${size}" height="${size}" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
    <path d="M2 22 L22 2" stroke="currentColor" stroke-width="1.6" opacity="0.5"/>
    <path d="M2 14 h8 v-5 h5 v-3" fill="none" stroke="currentColor" stroke-width="1.8"/>
    <circle cx="16" cy="8" r="2.4" fill="currentColor"/>
  </svg>`;
}
