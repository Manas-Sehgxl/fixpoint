import { findProduct } from "./products.js";
import { priceCart } from "./pricing.js";
import { lookupCoupon } from "./coupons.js";

const STORAGE_KEY = "tapri.cart.v1";

function readSaved(storage) {
  try {
    const raw = storage ? storage.getItem(STORAGE_KEY) : null;
    if (!raw) return null;
    const saved = JSON.parse(raw);
    return saved && Array.isArray(saved.lines) ? saved : null;
  } catch {
    return null;
  }
}

function isValidLine(line) {
  return line && findProduct(line.id) && Number.isInteger(line.qty) && line.qty > 0;
}

export function createCart(storage) {
  const listeners = new Set();
  let state = restore();

  function restore() {
    const saved = readSaved(storage);
    const lines = saved ? saved.lines.filter(isValidLine).map(({ id, qty }) => ({ id, qty })) : [];
    const found = saved && saved.coupon ? lookupCoupon(saved.coupon) : null;
    const coupon = found && found.ok ? found.coupon : null;
    return { lines, coupon, totals: priceCart(lines, coupon) };
  }

  function persist() {
    try {
      if (storage) {
        const coupon = state.coupon ? state.coupon.code : null;
        storage.setItem(STORAGE_KEY, JSON.stringify({ lines: state.lines, coupon }));
      }
    } catch {
      // Private browsing or a full quota: the cart still works for this visit.
    }
  }

  function emit() {
    for (const listener of listeners) listener(state);
  }

  function commit(next) {
    state = { ...next, totals: priceCart(next.lines, next.coupon) };
    persist();
    emit();
  }

  return {
    get state() {
      return state;
    },

    subscribe(listener) {
      listeners.add(listener);
      listener(state);
      return () => listeners.delete(listener);
    },

    addItem(id) {
      if (!findProduct(id)) return;
      const seen = state.lines.some((line) => line.id === id);
      const lines = seen
        ? state.lines.map((line) => (line.id === id ? { ...line, qty: line.qty + 1 } : line))
        : [...state.lines, { id, qty: 1 }];
      commit({ ...state, lines });
    },

    changeQty(id, delta) {
      const lines = state.lines.map((line) => (line.id === id ? { ...line, qty: line.qty + delta } : line));
      commit({ ...state, lines });
    },

    removeItem(id) {
      const lines = state.lines.filter((line) => line.id !== id);
      state = { ...state, lines };
      persist();
      emit();
    },

    applyCoupon(input) {
      const result = lookupCoupon(input);
      if (result.ok) commit({ ...state, coupon: result.coupon });
      return result;
    },

    removeCoupon() {
      commit({ ...state, coupon: null });
    },

    clear() {
      commit({ lines: [], coupon: null });
    },
  };
}
