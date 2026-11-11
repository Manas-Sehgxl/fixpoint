import { PRODUCTS } from "./products.js";
import { createCart } from "./cart.js";
import { searchProducts } from "./search.js";
import { renderMenu, renderBill } from "./ui.js";
import { placeOrder, validateOrder } from "./checkout.js";

const grid = document.getElementById("product-grid");
const countNode = document.getElementById("menu-count");
const searchInput = document.getElementById("search");
const searchForm = document.getElementById("search-form");
const panel = document.getElementById("bill");

function safeStorage() {
  try {
    const probe = "tapri.probe";
    window.localStorage.setItem(probe, "1");
    window.localStorage.removeItem(probe);
    return window.localStorage;
  } catch {
    return null;
  }
}

const storage = safeStorage();
const cart = createCart(storage);

const model = {
  cart: cart.state,
  view: "cart",
  query: "",
  couponInput: "",
  couponMessage: "",
  couponOk: false,
  checkoutErrors: {},
  details: { name: "", phone: "", address: "" },
  order: null,
  focus: null,
};

function paintMenu() {
  renderMenu(grid, countNode, searchProducts(PRODUCTS, model.query), model.query);
}

function paintBill() {
  renderBill(panel, model);
  if (model.focus) {
    const target = panel.querySelector(model.focus);
    if (target) target.focus();
    model.focus = null;
  }
}

cart.subscribe((state) => {
  model.cart = state;
  paintBill();
});

searchInput.addEventListener("input", () => {
  model.query = searchInput.value;
  paintMenu();
});

searchForm.addEventListener("submit", (event) => {
  event.preventDefault();
  model.query = searchInput.value;
  paintMenu();
});

grid.addEventListener("click", (event) => {
  const button = event.target.closest("[data-add]");
  if (!button) return;
  model.couponMessage = "";
  cart.addItem(button.dataset.add);
});

panel.addEventListener("click", (event) => {
  const step = event.target.closest("[data-step]");
  if (step) {
    const [id, delta] = step.dataset.step.split(":");
    model.couponMessage = "";
    cart.changeQty(id, Number(delta));
    return;
  }

  const remove = event.target.closest("[data-remove]");
  if (remove) {
    model.couponMessage = "";
    cart.removeItem(remove.dataset.remove);
    return;
  }

  const action = event.target.closest("[data-action]");
  if (!action) return;

  switch (action.dataset.action) {
    case "remove-coupon":
      model.couponInput = "";
      model.couponMessage = "";
      cart.removeCoupon();
      break;
    case "checkout":
      model.view = "checkout";
      model.checkoutErrors = {};
      paintBill();
      break;
    case "back-to-cart":
      model.view = "cart";
      paintBill();
      break;
    case "order-more":
      model.view = "cart";
      model.order = null;
      model.details = { name: "", phone: "", address: "" };
      paintBill();
      break;
    default:
      break;
  }
});

panel.addEventListener("input", (event) => {
  const input = event.target;
  if (input.id && input.id in model.details) model.details[input.id] = input.value;
  if (input.id === "coupon-code") model.couponInput = input.value;
});

panel.addEventListener("submit", (event) => {
  const form = event.target.closest("[data-form]");
  if (!form) return;
  event.preventDefault();

  if (form.dataset.form === "coupon") {
    const input = form.querySelector("#coupon-code");
    const result = cart.applyCoupon(input ? input.value : "");
    model.couponOk = result.ok;
    model.couponMessage = result.ok ? `${result.coupon.code} applied.` : result.message;
    model.couponInput = result.ok ? "" : input ? input.value : "";
    if (!result.ok) {
      model.focus = "#coupon-code";
      paintBill();
    }
    return;
  }

  const check = validateOrder(model.details);
  model.checkoutErrors = check.errors;
  if (!check.ok) {
    paintBill();
    const first = panel.querySelector("[aria-invalid='true']");
    if (first) first.focus();
    return;
  }

  const placed = placeOrder(cart, storage);
  if (!placed.ok) {
    model.view = "cart";
    paintBill();
    return;
  }

  model.order = { ...placed.order, details: check.details };
  model.view = "confirmed";
  paintBill();
});

paintMenu();
paintBill();
