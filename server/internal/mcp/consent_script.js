// Served as an external file (not inline) because the ingress CSP forbids
// inline scripts. Loaded by consent_template.html via a content-hashed
// <script src>.
//
// Three jobs:
//
//   1. Neutralise double-clicks on the consent controls. A second activation
//      while the first request is still pending sends the user to an authn
//      challenge that has already been consumed, producing "authn challenge
//      state not found or expired" (AIS-103).
//   2. Run each upstream Connect in a popup so the consent page keeps its
//      state — the multi-service case otherwise loses the whole page to a
//      provider login and comes back from scratch. Degrades to the plain
//      full-page form POST when popups are blocked or JS is unavailable.
//   3. Fan the page-level auto-refresh choice out to every card.
(function () {
  "use strict";

  // Names the popup, and — because window.name survives navigation within the
  // same window — is also how the document detects it is running *inside* that
  // popup after the provider redirected back here.
  var POPUP_NAME = "gram-connect-upstream";
  var POPUP_FEATURES = "popup=yes,width=560,height=760";
  var POPUP_MESSAGE = "gram-consent:upstream-finished";

  var origin = window.location.origin;

  function hasOpener() {
    try {
      return !!window.opener && !window.opener.closed;
    } catch (err) {
      // A cross-origin opener throws on access; either way it is not ours.
      return false;
    }
  }

  // Running inside the connect popup means the upstream leg has finished (or
  // was denied) and redirected back to the consent page. The decision belongs
  // on the page that opened it, so hand back and close rather than rendering a
  // second consent page inside a 560px window. If close() is refused the
  // document simply stays and remains usable.
  if (window.name === POPUP_NAME && hasOpener()) {
    try {
      window.opener.postMessage(POPUP_MESSAGE, origin);
    } catch (err) {
      // The opener navigated away or is gone; closing is still correct.
    }
    window.close();
    return;
  }

  // A connected first-party flow has no remaining consent step. Briefly show
  // confirmation, then close the tab opened by the dashboard. If the browser
  // declines window.close(), the completion message remains as a fallback.
  if (document.body.hasAttribute("data-auto-close")) {
    window.setTimeout(function () {
      window.close();
    }, 3000);
  }

  // Replace an element's contents with a spinner + label.
  function showPending(el, label) {
    el.textContent = "";
    var spinner = document.createElement("span");
    spinner.className =
      "mr-2 inline-block size-3 animate-spin rounded-full border-2 border-current border-r-transparent align-[-0.125em]";
    spinner.setAttribute("aria-hidden", "true");
    el.appendChild(spinner);
    el.appendChild(document.createTextNode(label));
  }

  // Give Access: block a repeat submit and show the pending state on the
  // button. The first submit proceeds normally.
  var form = document.querySelector("form[data-approve-form]");
  if (form) {
    var button = form.querySelector('button[type="submit"]');
    var submitted = false;
    form.addEventListener("submit", function (event) {
      if (submitted) {
        event.preventDefault();
        return;
      }
      submitted = true;
      if (!button) {
        return;
      }
      // Defer disabling to the next tick so the button's name/value
      // (action=approve) is still serialized into the outgoing form data —
      // disabling synchronously in the handler drops it in some browsers.
      window.setTimeout(function () {
        button.disabled = true;
        showPending(button, "Connecting…");
      }, 0);
    });
  }

  // Reload once the upstream leg finishes, so the card reflects the new
  // connection state. Both signals are needed: the popup posts a message when
  // it closes itself, and the poll covers a popup the user closed by hand or
  // one that ended on an error page carrying none of this script.
  var reloading = false;
  function reloadOnce() {
    if (reloading) return;
    reloading = true;
    window.location.reload();
  }

  window.addEventListener("message", function (event) {
    if (event.origin === origin && event.data === POPUP_MESSAGE) {
      reloadOnce();
    }
  });

  function watchPopup(popup) {
    var timer = window.setInterval(function () {
      if (popup.closed) {
        window.clearInterval(timer);
        reloadOnce();
      }
    }, 500);
  }

  // Connect / Reconnect: route the form POST into a popup so the consent page
  // survives the provider round-trip. The popup is opened synchronously in the
  // click handler — a popup opened any later is blocked — and the form target
  // is cleared afterwards so a blocked popup, or a later submit, still
  // navigates normally.
  var popupButtons = document.querySelectorAll("button[data-popup-action]");
  Array.prototype.forEach.call(popupButtons, function (popupButton) {
    popupButton.addEventListener("click", function () {
      var actionForm = popupButton.form;
      if (!actionForm) {
        return;
      }
      var popup = window.open("", POPUP_NAME, POPUP_FEATURES);
      if (!popup) {
        return;
      }
      actionForm.target = POPUP_NAME;
      popup.focus();
      watchPopup(popup);
      window.setTimeout(function () {
        actionForm.target = "";
      }, 0);
    });
  });

  // Connect / Reconnect and Refresh now each make an upstream request. Guard
  // both against repeat clicks and make their pending state visible.
  function guardActionButtons(selector, pendingLabel) {
    var buttons = document.querySelectorAll(selector);
    Array.prototype.forEach.call(buttons, function (actionButton) {
      actionButton.addEventListener("click", function (event) {
        if (actionButton.getAttribute("aria-disabled") === "true") {
          event.preventDefault();
          return;
        }
        actionButton.setAttribute("aria-disabled", "true");
        showPending(actionButton, pendingLabel);
        // Preserve the clicked button's action in the form submission, then
        // make the pending state native for keyboard and assistive technology.
        window.setTimeout(function () {
          actionButton.disabled = true;
        }, 0);
      });
    });
  }
  guardActionButtons("button[data-connect-link]", "Connecting…");
  guardActionButtons("button[data-refresh-link]", "Refreshing…");

  // Auto refresh: the page-level combobox drives every provider at once. A
  // change syncs each card's hidden auto_refresh input (so a subsequent
  // Connect carries the choice) and, when a stored session exists to update,
  // posts the hidden form to persist it immediately.
  var autoRefresh = document.querySelector("select[data-auto-refresh-select]");
  if (autoRefresh) {
    autoRefresh.addEventListener("change", function () {
      var value = autoRefresh.value === "on" ? "on" : "off";
      var inputs = document.querySelectorAll("input[data-auto-refresh-input]");
      Array.prototype.forEach.call(inputs, function (input) {
        input.value = value;
      });
      var refreshForm = document.getElementById("auto-refresh-form");
      if (
        refreshForm &&
        refreshForm.hasAttribute("data-auto-refresh-persist")
      ) {
        refreshForm.submit();
      }
    });
  }
})();
