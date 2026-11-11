import { formatRupees, lineTotal, FREE_DELIVERY_FROM, DELIVERY_FEE } from "./pricing.js";
import { findProduct } from "./products.js";
import { productArt } from "./art.js";
import { FEATURED_COUPON } from "./coupons.js";

function el(tag, props = {}, children = []) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (key === "class") node.className = value;
    else if (key === "text") node.textContent = value;
    else if (key === "html") node.innerHTML = value;
    else if (value !== null && value !== undefined) node.setAttribute(key, value);
  }
  for (const child of [].concat(children)) {
    if (child) node.appendChild(child);
  }
  return node;
}

function totalsRow(label, value, extra = "") {
  return el("div", { class: `bill-row ${extra}`.trim() }, [
    el("span", { class: "bill-label", text: label }),
    el("span", { class: "bill-value", "data-field": label.toLowerCase(), text: value }),
  ]);
}

export function renderMenu(grid, countNode, products, query) {
  grid.textContent = "";
  countNode.textContent = products.length === 1 ? "1 tea on the board" : `${products.length} teas on the board`;

  if (products.length === 0) {
    grid.appendChild(
      el("p", {
        class: "no-results",
        text: `No teas found for "${query}". Clear the search box to see the whole board.`,
      }),
    );
    return;
  }

  for (const product of products) {
    const card = el("article", { class: "card", "data-product": product.id }, [
      el("div", { class: "card-art", style: `background:${product.art.panel}`, html: productArt(product) }),
      el("div", { class: "card-body" }, [
        el("h3", { class: "card-name", text: product.name }),
        el("p", { class: "card-pack", text: product.pack }),
        el("p", { class: "card-notes", text: product.notes }),
        el("div", { class: "card-foot" }, [
          el("p", { class: "card-price", text: formatRupees(product.price) }),
          el("button", {
            class: "btn btn-add",
            type: "button",
            "data-add": product.id,
            "aria-label": `Add ${product.name} to the cart`,
            text: "Add to cart",
          }),
        ]),
      ]),
    ]);
    grid.appendChild(card);
  }
}

function cartLines(state) {
  const list = el("ul", { class: "lines" });
  for (const line of state.lines) {
    const product = findProduct(line.id);
    if (!product) continue;
    list.appendChild(
      el("li", { class: "line", "data-line": product.id }, [
        el("div", { class: "line-top" }, [
          el("h3", { class: "line-name", text: product.name }),
          el("p", { class: "line-each", text: `${product.pack} at ${formatRupees(product.price)} each` }),
        ]),
        el("div", { class: "line-controls" }, [
          el("div", { class: "stepper" }, [
            el("button", {
              class: "btn btn-step",
              type: "button",
              "data-step": `${product.id}:-1`,
              "aria-label": `Remove one ${product.name}`,
              text: "−",
            }),
            el("span", {
              class: "qty",
              "data-qty": product.id,
              "aria-label": `${product.name} quantity`,
              text: String(line.qty),
            }),
            el("button", {
              class: "btn btn-step",
              type: "button",
              "data-step": `${product.id}:1`,
              "aria-label": `Add one more ${product.name}`,
              text: "+",
            }),
          ]),
          el("p", { class: "line-total", "data-line-total": product.id, text: formatRupees(lineTotal(line)) }),
          el("button", {
            class: "btn btn-text",
            type: "button",
            "data-remove": product.id,
            "aria-label": `Remove ${product.name} from the cart`,
            text: "Remove",
          }),
        ]),
      ]),
    );
  }
  return list;
}

function totalsBlock(state) {
  const { subtotal, discount, delivery, total } = state.totals;
  const rows = [totalsRow("Subtotal", formatRupees(subtotal))];
  if (state.coupon) rows.push(totalsRow("Discount", formatRupees(-discount)));
  rows.push(totalsRow("Delivery", formatRupees(delivery)));
  rows.push(totalsRow("Total", formatRupees(total), "bill-row-total"));
  return el("div", { class: "totals" }, rows);
}

function couponForm(model) {
  const message = model.couponMessage
    ? el("p", {
        class: `coupon-msg ${model.couponOk ? "coupon-ok" : "coupon-bad"}`,
        role: "status",
        text: model.couponMessage,
      })
    : null;

  if (model.cart.coupon) {
    return el("div", { class: "coupon applied" }, [
      el("p", { class: "coupon-banner" }, [
        el("strong", { text: `${model.cart.coupon.code} applied` }),
        el("span", { text: `, ${model.cart.coupon.label}.` }),
      ]),
      el("button", {
        class: "btn btn-text",
        type: "button",
        "data-action": "remove-coupon",
        "aria-label": "Remove the coupon",
        text: "Remove the coupon",
      }),
      message,
    ]);
  }

  return el("form", { class: "coupon", "data-form": "coupon", novalidate: "" }, [
    el("label", { for: "coupon-code", text: "Coupon code" }),
    el("div", { class: "coupon-row" }, [
      el("input", {
        id: "coupon-code",
        name: "coupon",
        type: "text",
        autocomplete: "off",
        spellcheck: "false",
        placeholder: FEATURED_COUPON.code,
        value: model.couponInput ?? "",
      }),
      el("button", { class: "btn btn-apply", type: "submit", text: "Apply coupon" }),
    ]),
    message,
  ]);
}

function cartView(model) {
  const state = model.cart;
  const pieces = [];

  if (state.lines.length === 0) {
    pieces.push(el("p", { class: "bill-empty", text: "Your cart is empty. Add a pouch from the menu." }));
  } else {
    pieces.push(cartLines(state));
    pieces.push(totalsBlock(state));
  }

  pieces.push(
    el("p", {
      class: "bill-note",
      text: `Free delivery on orders of ${formatRupees(FREE_DELIVERY_FROM)} or more. Below that, delivery is ${formatRupees(DELIVERY_FEE)}.`,
    }),
  );
  pieces.push(couponForm(model));

  if (state.lines.length > 0) {
    pieces.push(
      el("button", {
        class: "btn btn-primary",
        type: "button",
        "data-action": "checkout",
        text: "Go to checkout",
      }),
    );
  }

  return pieces;
}

function field(id, label, model, type) {
  const error = model.checkoutErrors[id];
  const input =
    type === "textarea"
      ? el("textarea", {
          id,
          name: id,
          rows: "2",
          autocomplete: id === "address" ? "street-address" : "off",
          "aria-invalid": error ? "true" : null,
          "aria-describedby": error ? `${id}-error` : null,
        })
      : el("input", {
          id,
          name: id,
          type,
          autocomplete: id === "name" ? "name" : id === "phone" ? "tel" : "off",
          "aria-invalid": error ? "true" : null,
          "aria-describedby": error ? `${id}-error` : null,
          value: model.details[id] ?? "",
        });
  if (type === "textarea") input.value = model.details[id] ?? "";

  return el("div", { class: "field" }, [
    el("label", { for: id, text: label }),
    input,
    error ? el("p", { class: "field-error", id: `${id}-error`, text: error }) : null,
  ]);
}

function checkoutView(model) {
  const { total } = model.cart.totals;
  return [
    el("form", { class: "checkout", "data-form": "checkout", novalidate: "" }, [
      el("p", { class: "checkout-lead", text: "We deliver within the hour, cash or UPI at the door." }),
      field("name", "Your name", model, "text"),
      field("phone", "Phone number", model, "tel"),
      field("address", "Delivery address", model, "textarea"),
      el("div", { class: "totals" }, [totalsRow("Total", formatRupees(total), "bill-row-total")]),
      el("button", { class: "btn btn-primary", type: "submit", text: "Place the order" }),
      el("button", {
        class: "btn btn-text",
        type: "button",
        "data-action": "back-to-cart",
        text: "Back to the cart",
      }),
    ]),
  ];
}

function confirmedView(model) {
  const order = model.order;
  return [
    el("div", { class: "slip" }, [
      el("h3", { class: "slip-title", text: `Order ${order.number} is in` }),
      el("p", { class: "slip-lead", text: `Thanks ${order.details.name}. The stall is packing it now.` }),
      el("div", { class: "totals" }, [
        totalsRow("Order number", order.number),
        totalsRow("Items", String(order.totals.items)),
        totalsRow("Delivery", formatRupees(order.totals.delivery)),
        totalsRow("Total paid", formatRupees(order.totals.total), "bill-row-total"),
      ]),
      el("p", { class: "slip-address", text: `Going to ${order.details.address}` }),
      el("button", {
        class: "btn btn-primary",
        type: "button",
        "data-action": "order-more",
        text: "Order more chai",
      }),
    ]),
  ];
}

const HEADINGS = {
  cart: "Your order",
  checkout: "Checkout",
  confirmed: "Order slip",
};

export function renderBill(panel, model) {
  panel.textContent = "";
  panel.appendChild(el("div", { class: "pad-notch", "aria-hidden": "true" }));
  panel.appendChild(el("h2", { id: "bill-heading", class: "pad-title", text: HEADINGS[model.view] }));

  const body =
    model.view === "checkout" ? checkoutView(model) : model.view === "confirmed" ? confirmedView(model) : cartView(model);
  for (const piece of body) panel.appendChild(piece);
}
