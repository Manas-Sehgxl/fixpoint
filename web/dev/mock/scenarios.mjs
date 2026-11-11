// The stories the mock tells. Each one is a bug report against the demo shop
// with everything the loop would produce for it: the check, Kane's step
// remarks, the screenshots, and the patches each attempt makes.
//
// Field names below are mock-only; plan.mjs turns them into core types.

const URL = 'http://localhost:4000';

const ua = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Safari/537.36';
const phone = 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1';

const crumbs = (...list) => list.map(([kind, text, ago]) => ({ kind, text, ago_ms: ago }));

export const staleTotal = {
  key: 'stale-total',
  title: 'Total stays the same after removing an item',
  text: 'I removed the Elaichi Chai from my cart but the total did not go down. It still says ₹488 and I only have the Masala Chai left.',
  reporter: { name: 'Meera', email: '' },
  context: {
    page_url: `${URL}/#cart`, user_agent: ua, viewport_w: 1440, viewport_h: 900,
    breadcrumbs: crumbs(
      ['navigate', 'Opened "Tapri: chai, brewed slow"', 61000],
      ['click', 'Clicked "Add to cart" in "Masala Chai"', 48000],
      ['click', 'Clicked "Add to cart" in "Elaichi Chai"', 41000],
      ['click', 'Clicked "Remove" in "Elaichi Chai"', 12000],
    ),
    console_errors: [],
  },
  objective: `Go to ${URL}. Click the "Add to cart" button on the "Masala Chai" card. Click the "Add to cart" button on the "Elaichi Chai" card. Click the "Remove" button next to "Elaichi Chai" in the cart. Store the amount shown next to "Total" as 'observed'. Verify the order total shows ₹289.`,
  expected: 'Removing an item lowers the subtotal and the total by its price.',
  question: 'Which item did you remove, and what total did you expect to see?',
  remarks: [
    `Navigate to ${URL}`,
    'Click the "Add to cart" button on the "Masala Chai" card',
    'Click the "Add to cart" button on the "Elaichi Chai" card',
    'Click the "Remove" button next to "Elaichi Chai"',
    'Store the text next to "Total" as observed',
    'Verify the order total shows ₹289',
  ],
  observedFail: '₹488',
  observedPass: '₹289',
  failSummary: 'The total still shows ₹488 after Elaichi Chai is removed; it should be ₹289.',
  passSummary: 'The total shows ₹289 after Elaichi Chai is removed.',
  verdict: { confirmed: true, family: 'functional', category: 'state_not_updated', severity: 'high', one_liner: 'Cart totals are not recomputed when an item is removed.', confidence: '0.93' },
  shotItems: [['ELAICHI CHAI', '₹199'], ['MASALA CHAI', '₹249']],
  shotFail: { items: [['MASALA CHAI', '₹249']], cart: [['SUBTOTAL', '₹448'], ['DELIVERY', '₹40'], ['TOTAL', '₹488']], mark: 2, note: 'EXPECTED ₹289. SAW ₹488.' },
  shotPass: { items: [['MASALA CHAI', '₹249']], cart: [['SUBTOTAL', '₹249'], ['DELIVERY', '₹40'], ['TOTAL', '₹289']], mark: 2, note: 'TOTAL SHOWS ₹289.' },
  attempts: [{
    root_cause: 'removeItem() in js/cart.js splices the item out and re-renders the lines, but never calls renderTotals(), so the subtotal and total keep the values from before the removal.',
    summary: 'Recompute and render the totals at the end of removeItem().',
    files: [{
      path: 'js/cart.js', change: 'modified', additions: 1, deletions: 0,
      patch: `--- a/js/cart.js
+++ b/js/cart.js
@@ -44,6 +44,7 @@ export function removeItem(id) {
   state.items.splice(index, 1);
   save(state);
   renderLines(state.items);
+  renderTotals(computeTotals(state.items, state.coupon));
 }

 export function setQuantity(id, quantity) {
`,
    }],
    agent: { duration_ms: 41200, cost_usd: 0.18, turns: 7 },
  }],
};

export const couponCase = {
  key: 'coupon-case',
  title: 'Coupon chai10 is rejected as invalid',
  text: 'The banner says CHAI10 gives 10% off but when I type chai10 it says "Invalid code". Tried twice.',
  reporter: { name: '', email: 'arjun@example.com' },
  context: {
    page_url: `${URL}/#cart`, user_agent: phone, viewport_w: 390, viewport_h: 844,
    breadcrumbs: crumbs(
      ['navigate', 'Opened "Tapri: chai, brewed slow"', 95000],
      ['click', 'Clicked "Add to cart" in "Masala Chai"', 80000],
      ['input', 'Typed "chai10" in "Coupon code"', 31000],
      ['click', 'Clicked "Apply"', 29000],
      ['input', 'Typed " chai10" in "Coupon code"', 14000],
      ['click', 'Clicked "Apply"', 12000],
    ),
    console_errors: [],
  },
  objective: `Go to ${URL}. Click the "Add to cart" button on the "Masala Chai" card. Type " chai10" into the "Coupon code" field. Click the "Apply" button. Store the text next to "Discount" as 'observed'. Verify the "Discount" line shows -₹25.`,
  expected: 'Coupon codes are accepted whatever their case and surrounding spaces.',
  question: 'What exactly did you type into the coupon field?',
  remarks: [
    `Navigate to ${URL}`,
    'Click the "Add to cart" button on the "Masala Chai" card',
    'Type " chai10" into the "Coupon code" field',
    'Click the "Apply" button',
    'Store the text next to "Discount" as observed',
    'Verify the "Discount" line shows -₹25',
  ],
  observedFail: 'Invalid code',
  observedPass: '-₹25',
  failSummary: 'The coupon field shows "Invalid code" for " chai10"; the discount line should show -₹25.',
  passSummary: 'The discount line shows -₹25 after applying " chai10".',
  verdict: { confirmed: true, family: 'functional', category: 'input_validation', severity: 'medium', one_liner: 'Coupon codes are matched case-sensitively and without trimming.', confidence: '0.88' },
  shotItems: [['MASALA CHAI', '₹249']],
  shotFail: { items: [['MASALA CHAI', '₹249']], cart: [['SUBTOTAL', '₹249'], ['DISCOUNT', 'INVALID CODE'], ['DELIVERY', '₹40'], ['TOTAL', '₹289']], mark: 1, note: 'EXPECTED -₹25. SAW INVALID CODE.' },
  shotPass: { items: [['MASALA CHAI', '₹249']], cart: [['SUBTOTAL', '₹249'], ['DISCOUNT', '-₹25'], ['DELIVERY', '₹40'], ['TOTAL', '₹264']], mark: 1, note: 'DISCOUNT SHOWS -₹25.' },
  attempts: [{
    root_cause: 'findCoupon() in js/coupons.js compares the raw input with the stored code, so a leading space makes the match fail.',
    summary: 'Trim the input before looking the coupon up.',
    note: 'Kane still saw "Invalid code": the input is trimmed now, but "chai10" is lower case and the stored code is "CHAI10".',
    files: [{
      path: 'js/coupons.js', change: 'modified', additions: 1, deletions: 1,
      patch: `--- a/js/coupons.js
+++ b/js/coupons.js
@@ -8,7 +8,7 @@ const COUPONS = [
 ];

 export function findCoupon(input) {
-  return COUPONS.find((coupon) => coupon.code === input) || null;
+  return COUPONS.find((coupon) => coupon.code === input.trim()) || null;
 }

 export function applyCoupon(state, input) {
`,
    }],
    agent: { duration_ms: 38400, cost_usd: 0.16, turns: 6 },
  }, {
    root_cause: 'findCoupon() in js/coupons.js compares codes exactly. The stored code is upper case, so "chai10" never matches even after trimming; the comparison has to ignore case.',
    summary: 'Normalise the input with trim() and toUpperCase() before matching.',
    files: [{
      path: 'js/coupons.js', change: 'modified', additions: 2, deletions: 1,
      patch: `--- a/js/coupons.js
+++ b/js/coupons.js
@@ -8,7 +8,8 @@ const COUPONS = [
 ];

 export function findCoupon(input) {
-  return COUPONS.find((coupon) => coupon.code === input.trim()) || null;
+  const code = input.trim().toUpperCase();
+  return COUPONS.find((coupon) => coupon.code === code) || null;
 }

 export function applyCoupon(state, input) {
`,
    }],
    agent: { duration_ms: 29800, cost_usd: 0.14, turns: 5 },
  }],
  proofFiles: [{
    path: 'js/coupons.js', change: 'modified', additions: 2, deletions: 1,
    patch: `--- a/js/coupons.js
+++ b/js/coupons.js
@@ -8,7 +8,8 @@ const COUPONS = [
 ];

 export function findCoupon(input) {
-  return COUPONS.find((coupon) => coupon.code === input) || null;
+  const code = input.trim().toUpperCase();
+  return COUPONS.find((coupon) => coupon.code === code) || null;
 }

 export function applyCoupon(state, input) {
`,
  }],
};

export const qtyBelowOne = {
  key: 'qty-below-one',
  title: 'Quantity goes below one and the line total turns negative',
  text: 'Pressed minus on Masala Chai a few times and the quantity went to -1 with a line total of -₹249. The cart total went down too. [stub:stubborn]',
  reporter: { name: 'Kabir', email: 'kabir@example.com' },
  context: {
    page_url: `${URL}/#cart`, user_agent: ua, viewport_w: 1280, viewport_h: 800,
    breadcrumbs: crumbs(
      ['click', 'Clicked "Add to cart" in "Masala Chai"', 40000],
      ['click', 'Clicked "Decrease Masala Chai"', 22000],
      ['click', 'Clicked "Decrease Masala Chai"', 20500],
      ['error', 'Uncaught RangeError: Invalid array length at renderLines (cart.js:71)', 20400],
    ),
    console_errors: ['Uncaught RangeError: Invalid array length\n    at renderLines (http://localhost:4000/js/cart.js:71:18)\n    at decrement (http://localhost:4000/js/cart.js:57:3)'],
  },
  objective: `Go to ${URL}. Click the "Add to cart" button on the "Masala Chai" card. Click the "Decrease Masala Chai" button in the cart. Store the text of the cart as 'observed'. Verify the cart shows "Your cart is empty" and the total shows ₹0.`,
  expected: 'At quantity 1 the minus button removes the item, and totals never go negative.',
  question: 'How many times did you press minus, and what did the cart show afterwards?',
  remarks: [
    `Navigate to ${URL}`,
    'Click the "Add to cart" button on the "Masala Chai" card',
    'Click the "Decrease Masala Chai" button',
    'Store the cart text as observed',
    'Verify the cart shows "Your cart is empty" and the total shows ₹0',
  ],
  observedFail: 'Masala Chai × 0, ₹0',
  observedPass: 'Your cart is empty',
  failSummary: 'Masala Chai stays in the cart with quantity 0 instead of being removed.',
  passSummary: '',
  verdict: { confirmed: true, family: 'functional', category: 'boundary_value', severity: 'medium', one_liner: 'Quantity stepper allows values below one.', confidence: '0.81' },
  shotItems: [['MASALA CHAI', '₹249']],
  shotFail: { items: [['MASALA CHAI X 0', '₹0']], cart: [['SUBTOTAL', '₹0'], ['DELIVERY', '₹40'], ['TOTAL', '₹40']], mark: 2, note: 'EXPECTED AN EMPTY CART.' },
  attempts: [{
    root_cause: 'decrement() in js/cart.js subtracts one without a lower bound, so the quantity can reach 0 and then -1.',
    summary: 'Clamp the quantity at zero in decrement().',
    note: 'The quantity now stops at 0, but the item stays in the cart with a ₹0 line; the check expects it to be removed.',
    files: [{
      path: 'js/cart.js', change: 'modified', additions: 1, deletions: 1,
      patch: `--- a/js/cart.js
+++ b/js/cart.js
@@ -53,7 +53,7 @@ export function setQuantity(id, quantity) {
 export function decrement(id) {
   const item = state.items.find((entry) => entry.id === id);
   if (!item) return;
-  item.quantity -= 1;
+  item.quantity = Math.max(0, item.quantity - 1);
   save(state);
   renderLines(state.items);
   renderTotals(computeTotals(state.items, state.coupon));
`,
    }],
    agent: { duration_ms: 44100, cost_usd: 0.21, turns: 8 },
  }, {
    root_cause: 'The minus button in js/ui.js is always enabled, so it can be pressed at quantity 1. Disabling it there keeps the quantity at one or more.',
    summary: 'Disable the minus button when the quantity is 1.',
    note: 'Kane could not press minus at quantity 1, so the item could not be removed that way at all.',
    files: [{
      path: 'js/ui.js', change: 'modified', additions: 1, deletions: 1,
      patch: `--- a/js/ui.js
+++ b/js/ui.js
@@ -86,7 +86,7 @@ function stepper(item) {
   const wrap = document.createElement('div');
   wrap.className = 'stepper';
   const minus = button('−', \`Decrease \${item.name}\`);
-  minus.disabled = false;
+  minus.disabled = item.quantity <= 1;
   minus.addEventListener('click', () => decrement(item.id));
   const plus = button('+', \`Increase \${item.name}\`);
   plus.addEventListener('click', () => increment(item.id));
`,
    }],
    agent: { duration_ms: 52700, cost_usd: 0.24, turns: 9 },
  }, {
    root_cause: 'decrement() should remove the item when its quantity reaches zero. The filter replaces state.items, but renderTotals() is passed the array captured before the change.',
    summary: 'Remove the item from state.items when its quantity reaches zero.',
    note: 'The line disappears, but the total still counts it at ₹249, so the check fails on the total.',
    files: [{
      path: 'js/cart.js', change: 'modified', additions: 1, deletions: 0,
      patch: `--- a/js/cart.js
+++ b/js/cart.js
@@ -55,6 +55,7 @@ export function decrement(id) {
   const item = state.items.find((entry) => entry.id === id);
   if (!item) return;
   item.quantity -= 1;
+  if (item.quantity === 0) state.items = state.items.filter((entry) => entry !== item);
   save(state);
   renderLines(state.items);
   renderTotals(computeTotals(state.items, state.coupon));
`,
    }, {
      path: 'js/ui.js', change: 'modified', additions: 1, deletions: 1,
      patch: `--- a/js/ui.js
+++ b/js/ui.js
@@ -86,7 +86,7 @@ function stepper(item) {
   const wrap = document.createElement('div');
   wrap.className = 'stepper';
   const minus = button('−', \`Decrease \${item.name}\`);
-  minus.disabled = item.quantity <= 1;
+  minus.disabled = false;
   minus.addEventListener('click', () => decrement(item.id));
   const plus = button('+', \`Increase \${item.name}\`);
   plus.addEventListener('click', () => increment(item.id));
`,
    }],
    agent: { duration_ms: 61300, cost_usd: 0.29, turns: 11 },
  }],
  escalation: {
    stage: 'fix',
    message: 'Three attempts did not make the check pass, so every patch was rolled back and the app is as it was before.',
    remedy: 'Start with decrement() in js/cart.js: it changes item.quantity in place, and renderTotals() receives the items array from before the removal. Attempt 3 got closest.',
    retryable: true,
  },
};

export const freeDelivery = {
  key: 'free-delivery-threshold',
  title: 'Charged ₹40 delivery on an order of exactly ₹499',
  text: 'I ordered Masala Chai and Kadak Assam, exactly ₹499, and still got charged ₹40 delivery. The banner says free delivery on orders of ₹499 or more.',
  reporter: { name: 'Priya', email: 'priya@example.com' },
  context: {
    page_url: `${URL}/#checkout`, user_agent: ua, viewport_w: 1512, viewport_h: 945,
    breadcrumbs: crumbs(
      ['navigate', 'Opened "Tapri: chai, brewed slow"', 130000],
      ['click', 'Clicked "Add to cart" in "Masala Chai"', 118000],
      ['click', 'Clicked "Add to cart" in "Kadak Assam"', 109000],
      ['navigate', 'Went to #checkout', 60000],
      ['input', 'Typed in "Phone"', 41000],
    ),
    console_errors: [],
  },
  objective: `Go to ${URL}. Click the "Add to cart" button on the "Masala Chai" card. Click the "Add to cart" button on the "Kadak Assam" card. Store the amount shown next to "Delivery" as 'observed'. Verify the "Delivery" line shows Free and the order total shows ₹499.`,
  expected: 'Orders of ₹499 or more get free delivery.',
  question: 'What did the cart show for delivery, and what was in it?',
  remarks: [
    `Navigate to ${URL}`,
    'Click the "Add to cart" button on the "Masala Chai" card',
    'Click the "Add to cart" button on the "Kadak Assam" card',
    'Scroll to the cart summary',
    'Store the text next to "Delivery" as observed',
    'Verify the "Delivery" line shows Free and the total shows ₹499',
  ],
  observedFail: '₹40',
  observedPass: 'Free',
  failSummary: 'Delivery shows ₹40 on a ₹499 order, so the total is ₹539 instead of ₹499.',
  passSummary: 'Delivery shows Free and the total is ₹499.',
  verdict: { confirmed: true, family: 'functional', category: 'business_rule', severity: 'medium', one_liner: 'Free delivery threshold excludes orders of exactly ₹499.', confidence: '0.91' },
  shotItems: [['MASALA CHAI', '₹249'], ['KADAK ASSAM', '₹250']],
  shotFail: { items: [['MASALA CHAI', '₹249'], ['KADAK ASSAM', '₹250']], cart: [['SUBTOTAL', '₹499'], ['DELIVERY', '₹40'], ['TOTAL', '₹539']], mark: 1, note: 'EXPECTED FREE. SAW ₹40.' },
  shotPass: { items: [['MASALA CHAI', '₹249'], ['KADAK ASSAM', '₹250']], cart: [['SUBTOTAL', '₹499'], ['DELIVERY', 'FREE'], ['TOTAL', '₹499']], mark: 1, note: 'DELIVERY SHOWS FREE.' },
  attempts: [{
    root_cause: 'deliveryFor() in js/pricing.js compares with > instead of >=, so a subtotal of exactly ₹499 still pays the ₹40 fee even though the page promises free delivery from ₹499.',
    summary: 'Use >= so the free-delivery threshold includes ₹499 itself.',
    files: [{
      path: 'js/pricing.js', change: 'modified', additions: 1, deletions: 1,
      patch: `--- a/js/pricing.js
+++ b/js/pricing.js
@@ -3,7 +3,7 @@ export const FREE_DELIVERY_FROM = 499;

 export function deliveryFor(subtotal) {
   if (subtotal === 0) return 0;
-  return subtotal > FREE_DELIVERY_FROM ? 0 : DELIVERY_FEE;
+  return subtotal >= FREE_DELIVERY_FROM ? 0 : DELIVERY_FEE;
 }

 export function totalsFor(items, coupon) {
`,
    }],
    agent: { duration_ms: 6200, cost_usd: 0.12, turns: 4 },
  }],
};

export const searchCase = {
  key: 'search-case',
  title: 'Searching "kahwa" says no teas found',
  text: 'Typed kahwa in the search and it said No teas found, but Kashmiri Kahwa is right there on the menu when the search is empty.',
  reporter: { name: '', email: '' },
  context: {
    page_url: `${URL}/`, user_agent: ua, viewport_w: 1366, viewport_h: 768,
    breadcrumbs: crumbs(
      ['input', 'Typed "kahwa" in "Search teas"', 9000],
      ['input', 'Typed "Kahwa" in "Search teas"', 4000],
    ),
    console_errors: [],
  },
  objective: `Go to ${URL}. Type "kahwa" into the "Search teas" field. Store the names of the products shown as 'observed'. Verify a product card titled "Kashmiri Kahwa" is shown.`,
  expected: 'Search matches product names without regard to case.',
  question: 'What did you type into the search box, and what did you expect to see?',
  remarks: [
    `Navigate to ${URL}`,
    'Type "kahwa" into the "Search teas" field',
    'Store the product names shown as observed',
    'Verify a product card titled "Kashmiri Kahwa" is shown',
  ],
  observedFail: 'No teas found',
  failSummary: 'The menu shows "No teas found" for "kahwa"; a "Kashmiri Kahwa" card should be shown.',
  verdict: { confirmed: true, family: 'functional', category: 'search', severity: 'low', one_liner: 'Product search is case-sensitive.', confidence: 'high' },
  shotItems: [],
  shotFail: { search: 'KAHWA', empty: true, cart: [], mark: -1, note: 'EXPECTED KASHMIRI KAHWA. SAW NO TEAS FOUND.' },
  observedPass: 'Kashmiri Kahwa',
  passSummary: 'A "Kashmiri Kahwa" card is shown for "kahwa".',
  shotPass: { search: 'KAHWA', items: [], cart: [], mark: -1, note: 'KASHMIRI KAHWA IS SHOWN.' },
  attempts: [{
    root_cause: 'matches() in js/search.js compares the query with product names using includes() on the raw strings, so "kahwa" does not match "Kashmiri Kahwa".',
    summary: 'Lower-case both the query and the product name before comparing.',
    files: [{
      path: 'js/search.js', change: 'modified', additions: 2, deletions: 1,
      patch: `--- a/js/search.js
+++ b/js/search.js
@@ -1,6 +1,7 @@
 export function matches(product, query) {
-  return product.name.includes(query.trim());
+  const needle = query.trim().toLowerCase();
+  return product.name.toLowerCase().includes(needle);
 }
 
 export function search(products, query) {
`,
    }],
    agent: { duration_ms: 7400, cost_usd: 0.11, turns: 4 },
  }],
};

export const addToCartKahwa = {
  key: 'not-a-bug',
  title: 'Add to cart does nothing for Kashmiri Kahwa',
  text: 'Add to cart does nothing for Kashmiri Kahwa. I clicked it and the cart stayed empty.',
  reporter: { name: 'Sana', email: 'sana@example.com' },
  context: {
    page_url: `${URL}/`, user_agent: phone, viewport_w: 390, viewport_h: 844,
    breadcrumbs: crumbs(['click', 'Clicked "Add to cart" in "Kashmiri Kahwa"', 7000]),
    console_errors: ['Failed to load resource: net::ERR_INTERNET_DISCONNECTED'],
  },
  objective: `Go to ${URL}. Click the "Add to cart" button on the "Kashmiri Kahwa" card. Store the cart's item names as 'observed'. Verify the cart lists "Kashmiri Kahwa" with quantity 1.`,
  expected: 'Clicking "Add to cart" puts the tea in the cart.',
  question: 'Kashmiri Kahwa went into the cart when we tried it. Did the page show anything when you clicked, and were you online at the time?',
  remarks: [
    `Navigate to ${URL}`,
    'Click the "Add to cart" button on the "Kashmiri Kahwa" card',
    'Store the cart item names as observed',
    'Verify the cart lists "Kashmiri Kahwa" with quantity 1',
  ],
  observedPass: 'Kashmiri Kahwa × 1',
  passSummary: 'The cart lists Kashmiri Kahwa with quantity 1.',
  shotItems: [['KASHMIRI KAHWA', '₹320']],
  shotPass: { items: [['KASHMIRI KAHWA', '₹320']], cart: [['SUBTOTAL', '₹320'], ['DELIVERY', '₹40'], ['TOTAL', '₹360']], mark: -1, note: 'KASHMIRI KAHWA X 1 IN THE CART.' },
  replies: ['It was on the train, the signal was patchy. Seems fine now.'],
};

export const checkoutVague = {
  key: 'checkout-vague',
  title: 'Place order does not respond on a phone',
  text: 'checkout is broken on my phone',
  reporter: { name: '', email: 'dev.r@example.com' },
  context: {
    page_url: `${URL}/#checkout`, user_agent: phone, viewport_w: 390, viewport_h: 844,
    breadcrumbs: crumbs(
      ['click', 'Clicked "Add to cart" in "Adrak Chai"', 210000],
      ['click', 'Clicked "Add to cart" in "Tulsi Green"', 199000],
      ['click', 'Clicked "Add to cart" in "Filter Kaapi"', 190000],
      ['navigate', 'Went to #checkout', 150000],
      ['input', 'Typed in "Name"', 120000],
      ['input', 'Typed in "Phone"', 100000],
      ['input', 'Typed in "Address"', 80000],
      ['submit', 'Submitted "Checkout"', 52000],
      ['submit', 'Submitted "Checkout"', 30000],
    ),
    console_errors: [],
  },
  objective: `Go to ${URL}. Click the "Add to cart" button on the "Adrak Chai" card. Click the "Checkout" button. Type "Test Customer" into the "Name" field. Type "9876543210" into the "Phone" field. Type "12 MG Road, Bengaluru" into the "Address" field. Click the "Place order" button. Store the order number shown as 'observed'. Verify the page shows "Order placed".`,
  expected: 'Placing an order shows the confirmation with an order number.',
  question: 'What happened when you pressed "Place order": did the page change, show an error, or do nothing?',
  remarks: [
    `Navigate to ${URL}`,
    'Click the "Add to cart" button on the "Adrak Chai" card',
    'Click the "Checkout" button',
    'Fill in the name, phone and address fields',
    'Click the "Place order" button',
    'Store the order number as observed',
    'Verify the page shows "Order placed"',
  ],
  observedPass: 'Order TP-1042',
  passSummary: 'The page shows "Order placed" with order number TP-1042.',
  shotItems: [['ADRAK CHAI', '₹210']],
  shotPass: { items: [['ADRAK CHAI', '₹210']], cart: [['SUBTOTAL', '₹210'], ['DELIVERY', '₹40'], ['TOTAL', '₹250']], mark: -1, note: 'ORDER PLACED. TP-1042.' },
  replies: ['It just kept spinning after I pressed Place order. I had 3 teas in the cart.'],
  followUp: 'Thanks. We tried again with three teas and the order went through. Which three were in your cart, and did you change any quantities before checking out?',
};

export const priceMismatch = {
  key: 'price-mismatch',
  title: 'Kashmiri Kahwa costs more in the cart than on the menu',
  text: 'Kashmiri Kahwa is ₹320 on the menu but when I add it the cart line says ₹340.',
  reporter: { name: 'Ishaan', email: '' },
  context: {
    page_url: `${URL}/`, user_agent: ua, viewport_w: 1440, viewport_h: 900,
    breadcrumbs: crumbs(['click', 'Clicked "Add to cart" in "Kashmiri Kahwa"', 15000]),
    console_errors: [],
  },
  objective: `Go to ${URL}. Click the "Add to cart" button on the "Kashmiri Kahwa" card. Store the price on the cart line for "Kashmiri Kahwa" as 'observed'. Verify the cart line for "Kashmiri Kahwa" shows ₹320.`,
  expected: 'The cart line shows the same price as the menu.',
  question: 'Which tea was it, and what prices did you see on the menu and in the cart?',
  remarks: [
    `Navigate to ${URL}`,
    'Click the "Add to cart" button on the "Kashmiri Kahwa" card',
    'Store the cart line price as observed',
    'Verify the cart line for "Kashmiri Kahwa" shows ₹320',
  ],
  observedPass: '₹320',
  passSummary: 'The cart line for Kashmiri Kahwa shows ₹320, the same as the menu.',
  shotItems: [['KASHMIRI KAHWA', '₹320']],
  shotPass: { items: [['KASHMIRI KAHWA', '₹320']], cart: [['SUBTOTAL', '₹320'], ['DELIVERY', '₹40'], ['TOTAL', '₹360']], mark: -1, note: 'CART LINE SHOWS ₹320.' },
  problem: {
    stage: 'reproduce',
    message: 'Kane CLI could not start, so nothing was run against the app.',
    remedy: 'Kane CLI could not start. Check that you are signed in (`kane-cli whoami`, then `kane-cli login`) and that Chrome is installed.',
    retryable: true,
  },
  runError: 'Kane CLI exited with code 2 before the first step: not signed in. Run `kane-cli login`, then retry this report.',
};
