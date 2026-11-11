// Fixpoint report widget. One file, no dependencies, no network calls apart
// from the Fixpoint origin it was loaded from. It renders inside a shadow root
// so the host page and the widget cannot style each other.
//
// Embed it with:
//   <script src="http://localhost:7777/widget.js" data-fixpoint="http://localhost:7777" defer></script>
(function () {
  "use strict";

  if (window.__fixpointWidget) return;

  var script = document.currentScript;
  if (!script) {
    var all = document.querySelectorAll("script[src]");
    script = all[all.length - 1] || null;
  }

  var ORIGIN = (function () {
    var attr = script && script.getAttribute("data-fixpoint");
    if (attr) return String(attr).replace(/\/+$/, "");
    try {
      return new URL(script.src, location.href).origin;
    } catch (err) {
      return location.origin;
    }
  })();

  var MAX_CRUMBS = 20;
  var MAX_CONSOLE = 20;
  var VALUE_CUT = 40;
  var SESSION_KEY = "fixpoint.report";
  var STEPS = ["Received", "Reproduced", "Fix tested", "Fixed"];

  var SENSITIVE_AUTOCOMPLETE = /(^|\s)(name|given-name|additional-name|family-name|honorific-|nickname|email|username|tel|tel-|phone|street-address|address-line|address-level|postal-code|country|cc-|new-password|current-password|bday|sex|organization)/;
  var SENSITIVE_WORDS = /(pass|pwd|e-?mail|phone|mobile|tel|contact|first ?name|last ?name|full ?name|your name|address|pincode|postal|zip|card|cvv|cvc|\bpin\b|otp|upi|aadhaar|account|dob|birth)/i;
  var SENSITIVE_TYPES = { password: 1, email: 1, tel: 1 };

  function cut(text, max) {
    var s = String(text == null ? "" : text).replace(/\s+/g, " ").trim();
    return s.length > max ? s.slice(0, max - 1) + "…" : s;
  }

  function session(action, value) {
    try {
      if (action === "get") return window.sessionStorage.getItem(SESSION_KEY);
      if (action === "set") window.sessionStorage.setItem(SESSION_KEY, value);
      if (action === "clear") window.sessionStorage.removeItem(SESSION_KEY);
    } catch (err) {
      // Storage blocked: the widget still works, it just cannot resume.
    }
    return null;
  }

  // ---------------------------------------------------------------- breadcrumbs

  var crumbs = [];
  var consoleErrors = [];
  var host = null;

  function record(kind, text) {
    if (!text) return;
    crumbs.push({ kind: kind, text: cut(text, 300), at: Date.now() });
    if (crumbs.length > MAX_CRUMBS) crumbs.shift();
  }

  function insideWidget(event) {
    if (!host) return false;
    if (typeof event.composedPath === "function") {
      var path = event.composedPath();
      for (var i = 0; i < path.length; i++) {
        if (path[i] === host) return true;
      }
      return false;
    }
    return !!(event.target && event.target.nodeType === 1 && host.contains(event.target));
  }

  function labelText(node) {
    if (!node || node.nodeType !== 1) return "";
    var aria = node.getAttribute("aria-label");
    if (aria) return cut(aria, 60);
    var ids = node.getAttribute("aria-labelledby");
    if (ids) {
      var parts = [];
      ids.split(/\s+/).forEach(function (id) {
        var ref = document.getElementById(id);
        if (ref) parts.push(ref.textContent || "");
      });
      if (parts.join(" ").trim()) return cut(parts.join(" "), 60);
    }
    if (node.labels && node.labels.length) return cut(node.labels[0].textContent, 60);
    var alt = node.getAttribute("alt");
    if (alt) return cut(alt, 60);
    var title = node.getAttribute("title");
    if (title) return cut(title, 60);
    var own = cut(node.textContent, 60);
    if (own) return own;
    var placeholder = node.getAttribute("placeholder");
    if (placeholder) return cut(placeholder, 60);
    var name = node.getAttribute("name");
    return name ? cut(name, 60) : "";
  }

  function nearestHeading(node) {
    var scope = node;
    while (scope && scope !== document.body) {
      if (scope.matches && scope.matches("article, section, li, form, aside, dialog, [role='group']")) {
        var heading = scope.querySelector("h1, h2, h3, h4, h5, h6, legend");
        if (heading) {
          var text = cut(heading.textContent, 60);
          if (text) return text;
        }
      }
      scope = scope.parentElement;
    }
    return "";
  }

  function isSensitive(node) {
    var type = (node.getAttribute("type") || "").toLowerCase();
    if (SENSITIVE_TYPES[type]) return true;
    var auto = (node.getAttribute("autocomplete") || "").toLowerCase();
    if (auto && auto !== "off" && auto !== "on" && SENSITIVE_AUTOCOMPLETE.test(auto)) return true;
    var haystack = [
      node.getAttribute("name") || "",
      node.id || "",
      node.getAttribute("placeholder") || "",
      labelText(node),
    ].join(" ");
    return SENSITIVE_WORDS.test(haystack);
  }

  function onClick(event) {
    if (insideWidget(event)) return;
    var target = event.target;
    if (!target || target.nodeType !== 1) return;
    var actionable = target.closest(
      "button, a[href], [role='button'], [role='link'], [role='tab'], input[type='button'], input[type='submit'], summary, label, li[role='option']",
    );
    var node = actionable || target;
    var name = labelText(node);
    if (!name) return;
    var where = nearestHeading(node);
    record("click", where ? 'Clicked "' + name + '" in "' + where + '"' : 'Clicked "' + name + '"');
  }

  function onChange(event) {
    if (insideWidget(event)) return;
    var node = event.target;
    if (!node || node.nodeType !== 1) return;
    var tag = node.tagName.toLowerCase();
    if (tag !== "input" && tag !== "textarea" && tag !== "select") return;

    var name = labelText(node) || "a field";
    var type = (node.getAttribute("type") || "").toLowerCase();

    if (type === "checkbox" || type === "radio") {
      record("input", (node.checked ? 'Ticked "' : 'Unticked "') + name + '"');
      return;
    }
    if (isSensitive(node)) {
      record("input", 'Typed in "' + name + '"');
      return;
    }
    var value = tag === "select" ? (node.options[node.selectedIndex] || {}).text || node.value : node.value;
    if (!String(value || "").trim()) {
      record("input", 'Cleared "' + name + '"');
      return;
    }
    var verb = tag === "select" ? "Chose" : "Typed";
    record("input", verb + ' "' + cut(value, VALUE_CUT) + '" in "' + name + '"');
  }

  function onSubmit(event) {
    if (insideWidget(event)) return;
    var form = event.target;
    if (!form || form.nodeType !== 1) return;
    var name = form.getAttribute("aria-label") || nearestHeading(form) || form.getAttribute("name") || form.id;
    record("submit", name ? 'Submitted the "' + cut(name, 60) + '" form' : "Submitted a form");
  }

  function here() {
    return location.pathname + location.search + location.hash;
  }

  function onNavigate() {
    record("navigate", "Went to " + cut(here(), 120));
  }

  function noteError(message) {
    var text = cut(message, 300);
    if (!text) return;
    record("error", "Page error: " + text);
    if (consoleErrors.length < MAX_CONSOLE && consoleErrors.indexOf(text) === -1) consoleErrors.push(text);
  }

  function watchPage() {
    document.addEventListener("click", onClick, true);
    document.addEventListener("change", onChange, true);
    document.addEventListener("submit", onSubmit, true);
    window.addEventListener("popstate", onNavigate);
    window.addEventListener("hashchange", onNavigate);
    window.addEventListener("error", function (event) {
      if (event && event.message) noteError(event.message);
    });
    window.addEventListener("unhandledrejection", function (event) {
      var reason = event && event.reason;
      noteError(reason && reason.message ? reason.message : String(reason));
    });

    ["pushState", "replaceState"].forEach(function (method) {
      var original = history[method];
      if (typeof original !== "function") return;
      history[method] = function () {
        var result = original.apply(this, arguments);
        onNavigate();
        return result;
      };
    });

    var realError = console.error;
    if (typeof realError === "function") {
      console.error = function () {
        try {
          var parts = [];
          for (var i = 0; i < arguments.length; i++) {
            var arg = arguments[i];
            parts.push(arg && arg.message ? arg.message : String(arg));
          }
          var text = cut(parts.join(" "), 300);
          if (text && consoleErrors.length < MAX_CONSOLE && consoleErrors.indexOf(text) === -1) {
            consoleErrors.push(text);
          }
        } catch (err) {
          // Never let logging break the page.
        }
        return realError.apply(console, arguments);
      };
    }
  }

  function crumbsForSend() {
    var now = Date.now();
    return crumbs.map(function (crumb) {
      return { kind: crumb.kind, text: crumb.text, ago_ms: Math.max(0, now - crumb.at) };
    });
  }

  // ---------------------------------------------------------------------- view

  var STYLE = [
    ":host{all:initial}",
    "*{box-sizing:border-box;font-family:system-ui,-apple-system,'Segoe UI',Roboto,sans-serif}",
    ".launch{position:fixed;right:18px;bottom:18px;display:flex;align-items:center;gap:7px;",
    "background:#fff;color:#132238;border:1px solid #132238;border-radius:3px;padding:9px 13px;",
    "font-size:14px;font-weight:600;line-height:1;cursor:pointer;z-index:2147483000}",
    ".launch:hover{background:#F5F7FA}",
    ".launch .mark{color:#2743D2;display:flex}",
    ".panel{position:fixed;right:18px;bottom:18px;width:356px;max-width:calc(100vw - 24px);",
    "max-height:min(620px,calc(100vh - 32px));overflow:auto;background:#F5F7FA;color:#132238;",
    "border:1px solid #132238;border-radius:3px;padding:16px;z-index:2147483000}",
    "@media (max-width:420px){.panel{right:8px;left:8px;bottom:8px;width:auto}}",
    ".panel.in{animation:rise .12s ease-out}",
    "@keyframes rise{from{opacity:0;transform:translateY(6px)}to{opacity:1;transform:none}}",
    "@media (prefers-reduced-motion:reduce){.panel.in{animation:none}}",
    ".bar{display:flex;align-items:center;gap:7px;margin-bottom:12px}",
    ".bar .mark{color:#2743D2;display:flex}",
    ".brand{font-size:12px;font-weight:700;letter-spacing:.01em}",
    ".close{margin-left:auto;background:none;border:0;padding:2px 4px;font-size:13px;color:#4A5B75;",
    "text-decoration:underline;cursor:pointer}",
    "h2{font-size:17px;font-weight:700;margin:0 0 10px;line-height:1.25}",
    "label{display:block;font-size:12px;font-weight:600;color:#4A5B75;margin-bottom:4px}",
    "textarea,input{width:100%;font-size:14px;color:#132238;background:#fff;border:1px solid #8E9DB5;",
    "border-radius:3px;padding:8px 9px;line-height:1.4}",
    "textarea{min-height:84px;resize:vertical}",
    "textarea:focus-visible,input:focus-visible,button:focus-visible,a:focus-visible{outline:2px solid #2743D2;outline-offset:1px}",
    ".field{margin-bottom:11px}",
    ".disclose{font-size:12px;line-height:1.45;color:#4A5B75;margin:0 0 11px}",
    ".disclose button{background:none;border:0;padding:0;font-size:12px;font-weight:600;color:#2743D2;",
    "text-decoration:underline;cursor:pointer}",
    ".sent{margin:0 0 11px;border:1px solid #DDE5F0;background:#fff;border-radius:3px;padding:9px 10px}",
    ".sent h3{font-size:12px;margin:0 0 5px;color:#4A5B75;font-weight:600}",
    ".sent ul{margin:0 0 8px;padding-left:16px}",
    ".sent li{font-size:12px;line-height:1.5;color:#132238;word-break:break-word}",
    ".sent p{font-size:12px;margin:0 0 6px;color:#4A5B75}",
    ".send{width:100%;background:#2743D2;color:#fff;border:1px solid #2743D2;border-radius:3px;",
    "padding:10px;font-size:15px;font-weight:600;cursor:pointer}",
    ".send:disabled{opacity:.6;cursor:default}",
    ".ghost{width:100%;margin-top:8px;background:#fff;color:#132238;border:1px solid #132238;",
    "border-radius:3px;padding:9px;font-size:14px;font-weight:600;cursor:pointer}",
    ".err{font-size:13px;color:#C8321F;font-weight:600;margin:0 0 10px}",
    ".track{display:flex;gap:4px;margin:0 0 12px;padding:0;list-style:none}",
    ".track li{flex:1;font-size:10.5px;line-height:1.3;color:#4A5B75;text-align:center}",
    ".track .node{display:block;margin:0 auto 4px}",
    ".track li.on{color:#132238;font-weight:700}",
    ".headline{font-size:16px;font-weight:700;margin:0 0 4px;line-height:1.3}",
    ".detail{font-size:13.5px;line-height:1.45;margin:0 0 12px;color:#4A5B75}",
    ".meta{display:flex;align-items:center;gap:8px;margin:0 0 10px;font-size:12px;color:#4A5B75}",
    ".sim{border:1px solid #A8650A;color:#A8650A;border-radius:3px;padding:1px 5px;font-size:11px;font-weight:700}",
    ".thread{margin:0 0 12px;padding:0;list-style:none;border-top:1px solid #DDE5F0}",
    ".thread li{border-bottom:1px solid #DDE5F0;padding:8px 0}",
    ".thread .who{font-size:11px;font-weight:700;color:#4A5B75;margin:0 0 2px}",
    ".thread .what{font-size:13px;line-height:1.45;margin:0;word-break:break-word}",
    ".link{display:inline-block;margin-top:10px;font-size:12.5px;color:#2743D2}",
    ".foot{font-size:11.5px;color:#4A5B75;margin:10px 0 0;line-height:1.45}",
  ].join("");

  function mark(size) {
    var svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("viewBox", "0 0 24 24");
    svg.setAttribute("width", String(size));
    svg.setAttribute("height", String(size));
    svg.setAttribute("aria-hidden", "true");
    svg.setAttribute("class", "mark-svg");
    var diag = document.createElementNS(svg.namespaceURI, "path");
    diag.setAttribute("d", "M2 22 L22 2");
    diag.setAttribute("stroke", "currentColor");
    diag.setAttribute("stroke-width", "1.6");
    diag.setAttribute("opacity", "0.5");
    diag.setAttribute("fill", "none");
    var path = document.createElementNS(svg.namespaceURI, "path");
    path.setAttribute("d", "M2 14 h8 v-5 h5 v-3");
    path.setAttribute("fill", "none");
    path.setAttribute("stroke", "currentColor");
    path.setAttribute("stroke-width", "1.8");
    var dot = document.createElementNS(svg.namespaceURI, "circle");
    dot.setAttribute("cx", "16");
    dot.setAttribute("cy", "8");
    dot.setAttribute("r", "2.4");
    dot.setAttribute("fill", "currentColor");
    svg.appendChild(diag);
    svg.appendChild(path);
    svg.appendChild(dot);
    return svg;
  }

  function node(state) {
    var svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("viewBox", "0 0 16 16");
    svg.setAttribute("width", "14");
    svg.setAttribute("height", "14");
    svg.setAttribute("class", "node");
    svg.setAttribute("aria-hidden", "true");
    var ns = svg.namespaceURI;
    function add(tag, attrs) {
      var shape = document.createElementNS(ns, tag);
      Object.keys(attrs).forEach(function (key) {
        shape.setAttribute(key, attrs[key]);
      });
      svg.appendChild(shape);
      return shape;
    }
    if (state === "done") {
      add("circle", { cx: 8, cy: 8, r: 5, fill: "#0E7A4B" });
    } else if (state === "active") {
      add("circle", { cx: 8, cy: 8, r: 6, fill: "none", stroke: "#2743D2", "stroke-width": 1.6 });
      add("circle", { cx: 8, cy: 8, r: 2.6, fill: "#2743D2" });
    } else if (state === "waiting") {
      add("path", { d: "M8 2 L14 8 L8 14 L2 8 Z", fill: "none", stroke: "#A8650A", "stroke-width": 1.8 });
    } else if (state === "failed") {
      add("path", { d: "M3 3 L13 13 M13 3 L3 13", stroke: "#C8321F", "stroke-width": 2 });
    } else {
      add("circle", { cx: 8, cy: 8, r: 5, fill: "none", stroke: "#8E9DB5", "stroke-width": 1.4 });
    }
    return svg;
  }

  function make(tag, attrs, text) {
    var element = document.createElement(tag);
    if (attrs) {
      Object.keys(attrs).forEach(function (key) {
        if (attrs[key] !== null && attrs[key] !== undefined) element.setAttribute(key, attrs[key]);
      });
    }
    if (text !== undefined && text !== null) element.textContent = text;
    return element;
  }

  // ---------------------------------------------------------------- the widget

  var ui = {
    open: false,
    busy: false,
    error: "",
    disclosed: false,
    draft: "",
    email: "",
    replyDraft: "",
    justOpened: false,
    report: null, // the last PublicReport we were given
    saved: null, // {id, seq, status_url, events_url}
    stream: null,
    poll: 0,
  };

  var root = null;
  var panelEl = null;
  var launchEl = null;

  function trackStates(view) {
    var step = Math.max(1, Math.min(4, Number(view && view.step) || 1));
    var stage = (view && view.stage) || "received";
    var states = [];
    for (var i = 1; i <= 4; i++) {
      if (stage === "fixed") states.push("done");
      else if (i < step) states.push("done");
      else if (i > step) states.push("pending");
      else if (stage === "question") states.push("waiting");
      else if (stage === "handed_off" || stage === "trouble" || stage === "closed") states.push("failed");
      else states.push("active");
    }
    return states;
  }

  function buildTracker(view) {
    var list = make("ul", { class: "track" });
    var states = trackStates(view);
    STEPS.forEach(function (label, i) {
      var li = make("li", { class: states[i] === "pending" ? "" : "on" });
      li.appendChild(node(states[i]));
      li.appendChild(document.createTextNode(label));
      list.appendChild(li);
    });
    return list;
  }

  function disclosureBlock() {
    var wrap = make("div", { class: "sent" });
    wrap.appendChild(make("h3", null, "What will be sent"));
    wrap.appendChild(make("p", null, "Page address: " + location.href));
    wrap.appendChild(
      make("p", null, "Screen size: " + window.innerWidth + " by " + window.innerHeight + " pixels"),
    );

    if (crumbs.length) {
      wrap.appendChild(make("p", null, "Your last " + crumbs.length + " actions on this page:"));
      var list = make("ul");
      crumbs.forEach(function (crumb) {
        list.appendChild(make("li", null, crumb.text));
      });
      wrap.appendChild(list);
    } else {
      wrap.appendChild(make("p", null, "No actions recorded on this page yet."));
    }

    if (consoleErrors.length) {
      wrap.appendChild(make("p", null, "Errors this page logged:"));
      var errs = make("ul");
      consoleErrors.forEach(function (text) {
        errs.appendChild(make("li", null, text));
      });
      wrap.appendChild(errs);
    } else {
      wrap.appendChild(make("p", null, "This page logged no errors."));
    }

    wrap.appendChild(make("p", null, "Nothing you typed into a name, email, phone, address or card field is included."));
    return wrap;
  }

  function buildForm() {
    var frag = document.createDocumentFragment();
    frag.appendChild(make("h2", { id: "fixpoint-title" }, "Report a problem"));

    if (ui.error) frag.appendChild(make("p", { class: "err", role: "alert" }, ui.error));

    var form = make("form", { novalidate: "" });

    var textField = make("div", { class: "field" });
    textField.appendChild(make("label", { for: "fixpoint-text" }, "What went wrong?"));
    var textarea = make("textarea", {
      id: "fixpoint-text",
      name: "text",
      placeholder: "The total did not change when I removed a pouch.",
    });
    textarea.value = ui.draft;
    textarea.addEventListener("input", function () {
      ui.draft = textarea.value;
    });
    textField.appendChild(textarea);
    form.appendChild(textField);

    var emailField = make("div", { class: "field" });
    emailField.appendChild(make("label", { for: "fixpoint-email" }, "Your email, if you want an answer"));
    var email = make("input", { id: "fixpoint-email", name: "email", type: "email", autocomplete: "email" });
    email.value = ui.email;
    email.addEventListener("input", function () {
      ui.email = email.value;
    });
    emailField.appendChild(email);
    form.appendChild(emailField);

    var disclose = make("p", { class: "disclose" });
    disclose.appendChild(
      document.createTextNode(
        "Sent with your report: this page's address, your last " +
          crumbs.length +
          (crumbs.length === 1 ? " action" : " actions") +
          " on this page, and any errors it logged. ",
      ),
    );
    var toggle = make("button", { type: "button", "aria-expanded": ui.disclosed ? "true" : "false" }, ui.disclosed ? "Hide" : "Show");
    toggle.addEventListener("click", function () {
      ui.disclosed = !ui.disclosed;
      render();
    });
    disclose.appendChild(toggle);
    form.appendChild(disclose);

    if (ui.disclosed) form.appendChild(disclosureBlock());

    var send = make("button", { class: "send", type: "submit" }, ui.busy ? "Sending…" : "Send the report");
    if (ui.busy) send.disabled = true;
    form.appendChild(send);

    form.addEventListener("submit", function (event) {
      event.preventDefault();
      submitReport();
    });

    frag.appendChild(form);
    frag.appendChild(
      make("p", { class: "foot" }, "Fixpoint turns this into a browser check, reproduces it and tries a fix."),
    );
    return frag;
  }

  function buildStatus() {
    var report = ui.report;
    var frag = document.createDocumentFragment();
    var seq = (report && report.seq) || (ui.saved && ui.saved.seq) || 0;

    frag.appendChild(make("h2", { id: "fixpoint-title" }, seq ? "Report #" + seq + " received" : "Report received"));

    if (!report) {
      frag.appendChild(make("p", { class: "detail" }, "Fetching the latest on your report…"));
      return frag;
    }

    var view = report.view || {};
    frag.appendChild(buildTracker(view));

    if (report.simulated) {
      var meta = make("div", { class: "meta" });
      meta.appendChild(make("span", { class: "sim" }, "Simulated run"));
      meta.appendChild(make("span", null, "a stand-in ran this, not a real browser"));
      frag.appendChild(meta);
    }

    frag.appendChild(make("p", { class: "headline" }, view.headline || "Report received"));

    var thread = (report.thread || []).filter(function (message) {
      return message && message.text;
    });
    var lastFromUs = thread
      .filter(function (message) {
        return message.from !== "reporter";
      })
      .pop();
    var detailIsInThread = !!(view.detail && lastFromUs && lastFromUs.text === view.detail);
    if (view.detail && !detailIsInThread) frag.appendChild(make("p", { class: "detail" }, view.detail));

    if (thread.length) {
      var list = make("ul", { class: "thread" });
      thread.forEach(function (message) {
        var li = make("li");
        li.appendChild(make("p", { class: "who" }, message.from === "reporter" ? "You" : "Fixpoint"));
        li.appendChild(make("p", { class: "what" }, message.text || ""));
        list.appendChild(li);
      });
      frag.appendChild(list);
    }

    if (ui.error) frag.appendChild(make("p", { class: "err", role: "alert" }, ui.error));

    if (view.stage === "question") {
      var form = make("form", { novalidate: "" });
      var field = make("div", { class: "field" });
      field.appendChild(make("label", { for: "fixpoint-reply" }, "Your answer"));
      var reply = make("textarea", { id: "fixpoint-reply", name: "reply" });
      reply.value = ui.replyDraft;
      reply.addEventListener("input", function () {
        ui.replyDraft = reply.value;
      });
      field.appendChild(reply);
      form.appendChild(field);
      var send = make("button", { class: "send", type: "submit" }, ui.busy ? "Sending…" : "Send the answer");
      if (ui.busy) send.disabled = true;
      form.appendChild(send);
      form.addEventListener("submit", function (event) {
        event.preventDefault();
        submitReply();
      });
      frag.appendChild(form);
    }

    if (view.stage === "fixed") {
      var reload = make("button", { class: "send", type: "button" }, "Reload to get the fix");
      reload.addEventListener("click", function () {
        location.reload();
      });
      frag.appendChild(reload);
    }

    if (report.done) {
      var again = make("button", { class: "ghost", type: "button" }, "Report something else");
      again.addEventListener("click", function () {
        stopStream();
        session("clear");
        ui.report = null;
        ui.saved = null;
        ui.draft = "";
        ui.error = "";
        render();
      });
      frag.appendChild(again);
    }

    var statusURL = (ui.saved && ui.saved.status_url) || ORIGIN + "/r/" + encodeURIComponent(report.id || "");
    var link = make("a", { class: "link", href: statusURL, target: "_blank", rel: "noopener noreferrer" }, "Open the full status page");
    frag.appendChild(link);
    return frag;
  }

  function render() {
    if (!root) return;
    if (launchEl) launchEl.hidden = ui.open;
    if (panelEl) panelEl.remove();
    panelEl = null;
    if (!ui.open) return;

    panelEl = make("div", {
      class: ui.justOpened ? "panel in" : "panel",
      role: "dialog",
      "aria-modal": "false",
      "aria-labelledby": "fixpoint-title",
    });

    var bar = make("div", { class: "bar" });
    var markWrap = make("span", { class: "mark" });
    markWrap.appendChild(mark(16));
    bar.appendChild(markWrap);
    bar.appendChild(make("span", { class: "brand" }, "Fixpoint"));
    var close = make("button", { class: "close", type: "button" }, "Close");
    close.addEventListener("click", closePanel);
    bar.appendChild(close);
    panelEl.appendChild(bar);

    panelEl.appendChild(ui.saved || ui.report ? buildStatus() : buildForm());
    root.appendChild(panelEl);

    if (ui.justOpened) {
      ui.justOpened = false;
      var focusTarget =
        panelEl.querySelector("textarea") ||
        panelEl.querySelector("button.send") ||
        panelEl.querySelector("button") ||
        panelEl;
      try {
        focusTarget.focus({ preventScroll: true });
      } catch (err) {
        // Focus is a nicety, not a requirement.
      }
    }
  }

  function openPanel() {
    ui.open = true;
    ui.justOpened = true;
    ui.error = "";
    render();
    if (ui.saved && !ui.report) {
      refresh();
      startStream();
    }
  }

  function closePanel() {
    ui.open = false;
    render();
    if (launchEl) {
      try {
        launchEl.focus({ preventScroll: true });
      } catch (err) {
        // Ignore: the page may have moved focus itself.
      }
    }
  }

  // ------------------------------------------------------------------ requests

  function submitReport() {
    var text = String(ui.draft || "").trim();
    if (text.length < 5) {
      ui.error = "Add a few more words so we know what to look for.";
      render();
      return;
    }
    if (ui.busy) return;
    ui.busy = true;
    ui.error = "";
    render();

    var body = {
      text: text,
      reporter: { name: "", email: String(ui.email || "").trim() },
      context: {
        page_url: location.href,
        user_agent: navigator.userAgent,
        viewport_w: window.innerWidth || 0,
        viewport_h: window.innerHeight || 0,
        breadcrumbs: crumbsForSend(),
        console_errors: consoleErrors.slice(0, MAX_CONSOLE),
      },
    };

    post(ORIGIN + "/api/public/reports", body)
      .then(function (result) {
        ui.busy = false;
        if (!result.ok) {
          ui.error = result.message;
          render();
          return;
        }
        var data = result.data || {};
        ui.saved = {
          id: data.id,
          seq: data.seq,
          status_url: data.status_url || ORIGIN + "/r/" + encodeURIComponent(data.id || ""),
          events_url: data.events_url || ORIGIN + "/api/public/reports/" + encodeURIComponent(data.id || "") + "/events",
        };
        session("set", JSON.stringify(ui.saved));
        ui.draft = "";
        ui.report = null;
        render();
        refresh();
        startStream();
      })
      .catch(function () {
        ui.busy = false;
        ui.error = "The report could not be sent. Check your connection and try again.";
        render();
      });
  }

  function submitReply() {
    var text = String(ui.replyDraft || "").trim();
    if (text.length < 1 || !ui.saved) {
      ui.error = "Type your answer first.";
      render();
      return;
    }
    if (ui.busy) return;
    ui.busy = true;
    ui.error = "";
    render();

    post(ORIGIN + "/api/public/reports/" + encodeURIComponent(ui.saved.id) + "/reply", { text: text })
      .then(function (result) {
        ui.busy = false;
        if (!result.ok) {
          ui.error = result.message;
        } else {
          ui.replyDraft = "";
        }
        render();
        refresh();
      })
      .catch(function () {
        ui.busy = false;
        ui.error = "The answer could not be sent. Try again.";
        render();
      });
  }

  function post(url, body) {
    if (typeof fetch !== "function") {
      return Promise.resolve({ ok: false, message: "This browser cannot send the report." });
    }
    return fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    }).then(function (response) {
      return response
        .json()
        .catch(function () {
          return {};
        })
        .then(function (data) {
          if (response.ok) return { ok: true, data: data };
          var message =
            (data && data.error) ||
            (response.status === 429
              ? "Too many reports just now. Wait a few seconds and try again."
              : "The report could not be sent (error " + response.status + "). Try again.");
          return { ok: false, message: message };
        });
    });
  }

  function accept(report) {
    if (!report || !report.id) return;
    ui.report = report;
    if (ui.saved) {
      ui.saved.seq = report.seq || ui.saved.seq;
      session("set", JSON.stringify(ui.saved));
    }
    if (ui.open) render();
    if (report.done) stopStream();
  }

  function refresh() {
    if (!ui.saved || typeof fetch !== "function") return;
    fetch(ORIGIN + "/api/public/reports/" + encodeURIComponent(ui.saved.id))
      .then(function (response) {
        if (!response.ok) throw new Error("not ok");
        return response.json();
      })
      .then(accept)
      .catch(function () {
        // A failed refresh is not worth showing: the stream or the next poll retries.
      });
  }

  function startStream() {
    if (!ui.saved) return;
    stopStream();
    if (typeof window.EventSource !== "function") {
      ui.poll = window.setInterval(refresh, 5000);
      return;
    }
    try {
      var stream = new EventSource(ui.saved.events_url);
      stream.addEventListener("update", function (event) {
        try {
          accept(JSON.parse(event.data));
        } catch (err) {
          // Ignore a message we cannot read; the next one is usually fine.
        }
      });
      stream.addEventListener("error", function () {
        // EventSource reconnects by itself; poll as well in case it cannot.
        if (!ui.poll) ui.poll = window.setInterval(refresh, 8000);
      });
      ui.stream = stream;
    } catch (err) {
      ui.poll = window.setInterval(refresh, 5000);
    }
  }

  function stopStream() {
    if (ui.stream) {
      try {
        ui.stream.close();
      } catch (err) {
        // Already closed.
      }
      ui.stream = null;
    }
    if (ui.poll) {
      window.clearInterval(ui.poll);
      ui.poll = 0;
    }
  }

  // ------------------------------------------------------------------- startup

  function mount() {
    host = document.createElement("div");
    host.id = "fixpoint-widget";
    host.setAttribute("data-fixpoint-widget", "1");
    document.body.appendChild(host);
    root = host.attachShadow ? host.attachShadow({ mode: "open" }) : host;

    var style = document.createElement("style");
    style.textContent = STYLE;
    root.appendChild(style);

    launchEl = make("button", { class: "launch", type: "button", "aria-haspopup": "dialog" });
    var markWrap = make("span", { class: "mark" });
    markWrap.appendChild(mark(15));
    launchEl.appendChild(markWrap);
    launchEl.appendChild(make("span", null, "Report a problem"));
    launchEl.addEventListener("click", openPanel);
    root.appendChild(launchEl);

    root.addEventListener("keydown", function (event) {
      if (event.key === "Escape" && ui.open) {
        event.stopPropagation();
        closePanel();
      }
    });

    var saved = session("get");
    if (saved) {
      try {
        var parsed = JSON.parse(saved);
        if (parsed && parsed.id) {
          ui.saved = parsed;
          refresh();
          startStream();
        }
      } catch (err) {
        session("clear");
      }
    }

    render();

    window.__fixpointWidget = true;
  }

  function start() {
    watchPage();
    record("navigate", "Opened " + cut(here(), 120));
    if (document.body) mount();
    else document.addEventListener("DOMContentLoaded", mount);
  }

  start();
})();
