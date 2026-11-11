const COUPONS = [{ code: "CHAI10", percent: 10, label: "10% off your order" }];

export const FEATURED_COUPON = COUPONS[0];

export function lookupCoupon(input) {
  const code = String(input ?? "");
  if (!code.trim()) {
    return { ok: false, message: "Enter a coupon code first." };
  }
  const coupon = COUPONS.find((entry) => entry.code === code);
  if (!coupon) {
    return { ok: false, message: `"${code.trim()}" is not a valid coupon code.` };
  }
  return { ok: true, coupon };
}
