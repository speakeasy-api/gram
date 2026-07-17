// Served as an external file (not inline) because the ingress CSP forbids
// inline scripts. Loaded by consent_template.html via a content-hashed
// <script src>.
//
// Neutralises double-clicks on the consent controls. A second activation
// while the first request is still pending sends the user to an authn
// challenge that has already been consumed, producing "authn challenge
// state not found or expired" (AIS-103). Both the "Give Access" submit and
// the per-remote Connect/Reconnect links are guarded, and each swaps in a
// loading spinner so the pending state is visible.
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
    spinner.className = "spinner";
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

  // Connect / Reconnect: each link navigates to a freshly minted upstream
  // challenge. The first click navigates normally; a second click before
  // the page unloads would target a challenge that is about to be (or has
  // been) discarded, so we mark the link busy and swallow further clicks.
  var links = document.querySelectorAll("a[data-connect-link]");
  Array.prototype.forEach.call(links, function (link) {
    link.addEventListener("click", function (event) {
      if (link.getAttribute("aria-disabled") === "true") {
        event.preventDefault();
        return;
      }
      link.setAttribute("aria-disabled", "true");
      showPending(link, "Connecting…");
    });
  });
})();
