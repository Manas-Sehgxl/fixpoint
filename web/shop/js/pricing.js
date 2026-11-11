import { findProduct } from "./products.js";

export const DELIVERY_FEE = 40;
export const FREE_DELIVERY_FROM = 499;

export function formatRupees(amount) {
  const sign = amount < 0 ? "−" : "";
  return `${sign}₹${Math.abs(amount).toLocaleString("en-IN")}`;
}

export function lineTotal(line) {
  const product = findProduct(line.id);
  return product ? product.price * line.qty : 0;
}

export function deliveryFor(subtotal) {
  if (subtotal <= 0) return 0;
  return subtotal > FREE_DELIVERY_FROM ? 0 : DELIVERY_FEE;
}

export function priceCart(lines, coupon) {
  const subtotal = lines.reduce((sum, line) => sum + lineTotal(line), 0);
  const discount = coupon ? Math.round((subtotal * coupon.percent) / 100) : 0;
  const delivery = deliveryFor(subtotal);
  return {
    items: lines.reduce((count, line) => count + line.qty, 0),
    subtotal,
    discount,
    delivery,
    total: subtotal - discount + delivery,
  };
}
