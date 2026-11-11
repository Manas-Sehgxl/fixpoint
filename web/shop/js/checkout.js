const ORDER_KEY = "tapri.orderno.v1";
const FIRST_ORDER = 1041;

export function validateOrder(details) {
  const errors = {};
  const name = String(details.name ?? "").trim();
  const phone = String(details.phone ?? "").trim();
  const address = String(details.address ?? "").trim();

  if (name.length < 2) errors.name = "Tell us the name for the order.";
  if (phone.replace(/\D/g, "").length !== 10) errors.phone = "A phone number is 10 digits.";
  if (address.length < 10) errors.address = "Add a delivery address we can find.";

  return { ok: Object.keys(errors).length === 0, errors, details: { name, phone, address } };
}

export function formatOrderNumber(n) {
  return `TAP-${n}`;
}

export function nextOrderNumber(storage) {
  let last = FIRST_ORDER;
  try {
    const saved = storage ? Number(storage.getItem(ORDER_KEY)) : 0;
    if (Number.isFinite(saved) && saved >= FIRST_ORDER) last = saved;
  } catch {
    // No storage: start the pad again at the first slip.
  }
  const next = last + 1;
  try {
    if (storage) storage.setItem(ORDER_KEY, String(next));
  } catch {
    // Nothing to do: the order still goes through.
  }
  return formatOrderNumber(next);
}

export function placeOrder(cart, storage) {
  const totals = cart.state.totals;
  if (totals.items <= 0) {
    return { ok: false, errors: { address: "" }, message: "Add something to the cart first." };
  }
  const order = {
    number: nextOrderNumber(storage),
    items: cart.state.lines.map((line) => ({ ...line })),
    totals,
    coupon: cart.state.coupon ? cart.state.coupon.code : null,
  };
  cart.clear();
  return { ok: true, order };
}
