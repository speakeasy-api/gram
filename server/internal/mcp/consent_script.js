// Served as an external file (not inline) because the ingress CSP forbids
// inline scripts. Loaded by consent_template.html via a content-hashed
// <script src>.
//
// Two jobs:
//
//   1. Neutralise double-clicks on the consent controls. A second activation
//      while the first request is still pending sends the user to an authn
//      challenge that has already been consumed, producing "authn challenge
//      state not found or expired" (AIS-103).
//   2. Fan the page-level auto-refresh choice out to every card.
//
// Connect deliberately stays a full-page form POST. Running it in a popup kept
// the page's state through the provider round trip, but handed the provider a
// window.opener onto this consent screen — a reverse-tabnabbing target on the
// one page where a spoof is worth the most — and reloading the parent when the
// popup closed discarded any pending tool selection, silently widening the
// grant back to "all tools".
(function () {
  "use strict";

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

  // Connect / Reconnect and Refresh each make an upstream request. Guard both
  // against repeat clicks and make their pending state visible.
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

  // Session length is stated on the summary line so it is visible without
  // opening the configuration disclosure; keep the two in step when the
  // control inside the disclosure changes.
  var sessionDuration = document.querySelector(
    'select[name="session_duration_hours"]',
  );
  var sessionDurationLabel = document.querySelector(
    "[data-session-duration-label]",
  );
  if (sessionDuration && sessionDurationLabel) {
    sessionDuration.addEventListener("change", function () {
      var option = sessionDuration.options[sessionDuration.selectedIndex];
      var short = option && option.getAttribute("data-short-label");
      if (short) {
        sessionDurationLabel.textContent = short;
      }
    });
  }

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
