// Served as an external file (not inline) because the ingress CSP forbids
// inline scripts. Loaded by consent_template.html via a content-hashed
// <script src>.
//
// Main jobs:
//
//   1. Neutralise double-clicks on the consent controls. A second activation
//      while the first request is still pending sends the user to an authn
//      challenge that has already been consumed, producing "authn challenge
//      state not found or expired" (AIS-103).
//   2. Fan the page-level auto-refresh choice out to every card.
//   3. Check identity-chained cards' exchanges in the background.
//
// Connect deliberately stays a full-page form POST. Running it in a popup kept
// the page's state through the provider round trip, but handed the provider a
// window.opener onto this consent screen — a reverse-tabnabbing target on the
// one page where a spoof is worth the most — and reloading the parent when the
// popup closed discarded any pending tool selection, silently widening the
// grant back to "all tools".
(function () {
  "use strict";

  document
    .querySelectorAll("[data-permission-details]")
    .forEach(function (button) {
      button.addEventListener("click", function () {
        var popover = document.getElementById(
          button.getAttribute("popovertarget"),
        );
        var rect = button.getBoundingClientRect();
        popover.style.left =
          Math.max(12, Math.min(rect.left, window.innerWidth - 332)) + "px";
        popover.style.top =
          Math.max(12, Math.min(rect.bottom + 8, window.innerHeight - 280)) +
          "px";
      });
    });

  var delegationForm = document.querySelector("[data-delegation-form]");
  if (delegationForm) {
    var integratedDelegation = delegationForm.hasAttribute(
      "data-integrated-delegation",
    );
    var approvalForm = document.querySelector("[data-approve-form]");
    var delegationFinished = false;
    var grantButton = integratedDelegation
      ? document.querySelector("#consent-approve-button")
      : delegationForm.querySelector("[data-grant-agents]");
    var skipButton = delegationForm.querySelector("[data-skip-delegation]");
    var delegationError = delegationForm.querySelector(
      "[data-delegation-error]",
    );
    var delegationBusy = false;
    var delegationEnabled = delegationForm.querySelector(
      "[data-delegation-enabled]",
    );
    var delegationToggle = delegationForm.querySelector(
      "[data-delegation-toggle]",
    );
    if (delegationToggle)
      delegationToggle.addEventListener("click", function () {
        var expanded =
          delegationToggle.getAttribute("aria-expanded") === "true";
        delegationToggle.setAttribute("aria-expanded", String(!expanded));
        delegationForm
          .querySelector("[data-delegation-header]")
          .classList.toggle("bg-accent", !expanded);
        delegationForm.querySelector("#delegation-agent-choices").hidden =
          expanded;
        delegationToggle.querySelector(
          "[data-delegation-chevron]",
        ).style.transform = expanded ? "" : "rotate(180deg)";
      });
    function updateDelegationSelection() {
      var names = Array.from(
        delegationForm.querySelectorAll(
          'input[name="agent_id"]:checked:not(:disabled)',
        ),
      ).map(function (input) {
        return input.dataset.agentName;
      });
      var header = delegationForm.querySelector("[data-delegation-header]");
      if (header && delegationEnabled) {
        if (!names.length) {
          header.hidden = true;
          delegationEnabled.checked = false;
          delegationForm.querySelector("#delegation-agent-choices").hidden =
            false;
        } else if (header.hidden) {
          header.hidden = false;
          delegationEnabled.checked = true;
          delegationToggle.setAttribute("aria-expanded", "true");
          header.classList.add("bg-accent");
          delegationToggle.querySelector(
            "[data-delegation-chevron]",
          ).style.transform = "rotate(180deg)";
        }
      }
      var summary = delegationForm.querySelector("[data-delegation-summary]");
      if (summary)
        summary.textContent =
          names.length === 0
            ? ""
            : names.length === 1
              ? names[0]
              : names.length === 2
                ? names.join(" and ")
                : names.length === 3
                  ? names.slice(0, 2).join(", ") + ", and " + names[2]
                  : names.slice(0, 2).join(", ") +
                    ", and " +
                    (names.length - 2) +
                    " others";
      if (integratedDelegation) return;
      grantButton.disabled =
        delegationBusy ||
        (delegationEnabled && !delegationEnabled.checked) ||
        !delegationForm.querySelector(
          'input[name="agent_id"]:checked:not(:disabled)',
        );
      skipButton.disabled = delegationBusy;
    }
    async function delegationRequest(action, agent) {
      var body = new URLSearchParams();
      delegationForm
        .querySelectorAll('input[type="hidden"]')
        .forEach(function (input) {
          body.set(input.name, input.value);
        });
      body.delete("agent_id");
      body.set("action", action);
      if (agent) {
        body.set("agent_id", agent.value);
        body.set("binding_id", agent.dataset.bindingId);
      }
      var response = await fetch(
        delegationForm.dataset.actionUrl || delegationForm.action,
        {
          method: "POST",
          body: body,
          credentials: "same-origin",
        },
      );
      if (!response.ok)
        throw new Error(
          "The connection or your permissions changed. Reload to review current assignments. Successfully granted agents keep their connection.",
        );
    }
    async function finishDelegation(grant) {
      if (delegationBusy) return;
      delegationBusy = true;
      updateDelegationSelection();
      delegationError.hidden = true;
      try {
        if (grant && (!delegationEnabled || delegationEnabled.checked)) {
          var selected = Array.from(
            delegationForm.querySelectorAll(
              'input[name="agent_id"]:checked:not(:disabled)',
            ),
          );
          for (var agent of selected) {
            await delegationRequest("delegate_grant", agent);
            agent.checked = false;
            agent.disabled = true;
          }
        }
        await delegationRequest("finish_delegation");
        if (integratedDelegation) {
          delegationFinished = true;
          approvalForm.requestSubmit(grantButton);
        } else {
          window.location.assign(delegationForm.dataset.continueUrl);
        }
      } catch (error) {
        delegationError.textContent = error.message;
        delegationError.hidden = false;
      } finally {
        delegationBusy = false;
        updateDelegationSelection();
      }
    }
    delegationForm.addEventListener("change", updateDelegationSelection);
    (integratedDelegation ? approvalForm : delegationForm).addEventListener(
      "submit",
      function (event) {
        if (delegationFinished) return;
        event.preventDefault();
        event.stopImmediatePropagation();
        finishDelegation(true);
      },
      true,
    );
    if (skipButton)
      skipButton.addEventListener("click", function () {
        finishDelegation(false);
      });
    updateDelegationSelection();
    if (!integratedDelegation) return;
  }

  // A connected first-party flow has no remaining consent step. Briefly show
  // confirmation, then close the tab opened by the dashboard. If the browser
  // declines window.close(), the completion message remains as a fallback.
  if (document.body.hasAttribute("data-auto-close")) {
    window.setTimeout(function () {
      window.close();
    }, 3000);
  }

  // A card whose automatic verification was still running when the callback
  // redirected reads "Connected · Verifying…". First-party pages can
  // safely reload until the callback probe's absolute deadline; interactive
  // consent pages do not poll because a reload would discard tool selections.
  var cardList = document.querySelector("[data-verify-deadline-ms]");
  if (cardList && document.querySelector('[data-validation="pending"]')) {
    // The server only renders this attribute while its deadline is live.
    // Let the next render stop polling: the browser's clock may be skewed.
    window.setTimeout(function () {
      window.location.reload();
    }, 2000);
  }

  // A gateway's server list keeps the open state the person left it in, so
  // a Connect from the list, which leaves for the provider and comes back,
  // returns to the list rather than to its collapsed summary.
  var gatewayList = document.querySelector("details[data-gateway-list]");
  if (gatewayList) {
    var listKey =
      "consent-gateway-list:" + gatewayList.getAttribute("data-state");
    try {
      var remembered = window.sessionStorage.getItem(listKey);
      if (remembered === "open") {
        gatewayList.open = true;
      } else if (remembered === "closed") {
        gatewayList.open = false;
      }
    } catch (_) {
      // Storage can be unavailable; the server's default stands.
    }
    gatewayList.addEventListener("toggle", function () {
      try {
        window.sessionStorage.setItem(
          listKey,
          gatewayList.open ? "open" : "closed",
        );
      } catch (_) {
        // Storage can be unavailable; nothing to remember then.
      }
    });
  }

  // Hide and Show update in place; without the script the forms post and redirect.
  var visibleServers = document.querySelector("[data-gateway-visible-list]");
  var hiddenServers = document.querySelector("[data-gateway-hidden-list]");
  if (visibleServers && hiddenServers) {
    var hiddenSection = document.querySelector("[data-gateway-hidden-section]");
    var allHidden = document.querySelector("[data-gateway-all-hidden]");
    var gatewaySummary = document.querySelector("[data-gateway-summary]");
    var hiddenCount = document.querySelector("[data-gateway-hidden-count]");
    var visibilityError = document.querySelector(
      "[data-gateway-visibility-error]",
    );

    var refreshGatewayCounts = function () {
      var visibleRows = visibleServers.querySelectorAll(
        "[data-gateway-server]",
      );
      var hiddenRows = hiddenServers.querySelectorAll("[data-gateway-server]");
      var ready = Array.prototype.filter.call(visibleRows, function (row) {
        return row.getAttribute("data-gateway-server-ready") === "true";
      }).length;
      var summary =
        ready +
        " of " +
        visibleRows.length +
        (visibleRows.length === 1 ? " server" : " servers") +
        " connected";
      if (hiddenRows.length) summary += " · " + hiddenRows.length + " hidden";
      gatewaySummary.textContent = summary;
      hiddenCount.textContent = String(hiddenRows.length);
      visibleServers.hidden = visibleRows.length === 0;
      allHidden.hidden = visibleRows.length !== 0;
      hiddenSection.hidden = hiddenRows.length === 0;
    };

    var moveGatewayRow = function (row, hide) {
      Array.prototype.forEach.call(
        row.querySelectorAll(
          "[data-gateway-server-status], [data-gateway-server-actions]",
        ),
        function (el) {
          el.hidden = hide;
        },
      );
      var toggle = row.querySelector("[data-gateway-visibility-toggle]");
      toggle.value = hide ? "show_server" : "hide_server";
      toggle.textContent = hide ? "Show" : "Hide";
      toggle.title = hide
        ? "Make this server available to your tools again"
        : "Hide this server from your tools";
      (hide ? hiddenServers : visibleServers).appendChild(row);
    };

    document.addEventListener("submit", async function (event) {
      var visibilityForm = event.target;
      if (!visibilityForm.matches("[data-gateway-visibility-form]")) return;
      event.preventDefault();
      var toggle = visibilityForm.querySelector(
        "[data-gateway-visibility-toggle]",
      );
      if (toggle.disabled) return;
      var hide = toggle.value === "hide_server";
      var body = new URLSearchParams(new FormData(visibilityForm));
      body.set("action", toggle.value);
      toggle.disabled = true;
      visibilityError.hidden = true;
      var moved = false;
      try {
        var response = await fetch(visibilityForm.getAttribute("action"), {
          method: "POST",
          credentials: "same-origin",
          body: body,
          headers: { Accept: "application/json" },
        });
        if (!response.ok) throw new Error(String(response.status));
        moveGatewayRow(visibilityForm.closest("[data-gateway-server]"), hide);
        moved = true;
        refreshGatewayCounts();
      } catch (_) {
        visibilityError.textContent =
          "Couldn't update that server. Reload the page and try again.";
        visibilityError.hidden = false;
      } finally {
        toggle.disabled = false;
        if (moved) toggle.focus();
      }
    });
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
    var discoverySelect = document.querySelector(
      'select[name="discovery_mode"]',
    );
    var discoveryKey = "gram-consent-discovery-v1:" + form.elements.state.value;
    if (discoverySelect) {
      try {
        var savedMode = sessionStorage.getItem(discoveryKey);
        if (
          savedMode === "" ||
          savedMode === "direct" ||
          savedMode === "progressive" ||
          (savedMode === "code_mode" &&
            discoverySelect.querySelector('option[value="code_mode"]'))
        ) {
          discoverySelect.value = savedMode;
        }
      } catch (_) {
        /* Storage can be unavailable. */
      }
      discoverySelect.addEventListener("change", function () {
        try {
          sessionStorage.setItem(discoveryKey, discoverySelect.value);
        } catch (_) {
          /* The current form still submits the selected mode. */
        }
      });
    }
    form.addEventListener("submit", function (event) {
      if (submitted || (button && button.disabled)) {
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

  // Connect / Reconnect, Refresh and Re-check each make an upstream request; guard repeat clicks and show pending.
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
  guardActionButtons("button[data-validate-link]", "Checking…");

  // Identity-chained cards render neutral, so they read right without script.
  // Check each exchange in the background and settle the card in place; any
  // failure keeps the neutral copy and the separate sign-in fallback.
  var connectedSummary = document.querySelector("[data-connected-summary]");
  var chainActionURL = (function () {
    var services = document.querySelector("[data-service-connections]");
    return services ? services.getAttribute("data-action-url") : "";
  })();

  function settleChainCheck(status, card, result) {
    var state = result ? result.status : "";
    if (state !== "connected" && state !== "rejected") {
      state = "unknown";
    }
    status.setAttribute("data-chain-check", state);
    if (state === "connected") {
      status.className = "text-xs text-default-success";
      status.textContent = "Service connected through your identity provider";
      if (connectedSummary) {
        var count =
          Number(connectedSummary.getAttribute("data-connected-count")) + 1;
        connectedSummary.setAttribute("data-connected-count", String(count));
        connectedSummary.textContent =
          count +
          " of " +
          connectedSummary.getAttribute("data-connected-total") +
          " connected";
      }
      return;
    }
    if (state === "rejected") {
      status.className = "text-xs text-default-warning";
      status.textContent =
        result.message ||
        "Your identity provider sign-in wasn't accepted. Use a separate sign-in.";
      var fallback = card.querySelector("button[data-connect-fallback]");
      if (fallback && fallback.getAttribute("aria-disabled") !== "true") {
        fallback.className =
          "bg-primary text-primary-foreground interact:bg-foreground flex h-8 items-center px-3 text-sm";
        fallback.removeAttribute("data-connect-fallback");
        fallback.textContent = "Connect";
      }
      signalChainRejected();
      return;
    }
    status.className = "text-xs text-muted-foreground";
    status.textContent = "Managed by your identity provider";
  }

  // Tells the tool access island that a chained service was refused. The
  // island defers its inventory only when every service is chain-only, and
  // on such a page each chained card serves the upstream the inventory
  // needs, so one refusal means the tools can't load after approval either.
  // Signing in separately reloads the page with that service connected,
  // which the server no longer renders as deferred.
  function signalChainRejected() {
    document.documentElement.setAttribute(
      "data-consent-chain-rejected",
      "true",
    );
    document.dispatchEvent(new CustomEvent("gram:consent-chain-rejected"));
  }

  var chainChecks = document.querySelectorAll('[data-chain-check="pending"]');
  Array.prototype.forEach.call(chainChecks, function (status) {
    var card = status.closest("[data-remote-client]");
    var state = card && card.querySelector('input[name="state"]');
    var csrf = card && card.querySelector('input[name="csrf_token"]');
    if (!chainActionURL || !state || !csrf) {
      return;
    }
    showPending(status, "Connecting through your identity provider…");
    fetch(chainActionURL, {
      method: "POST",
      credentials: "same-origin",
      body: new URLSearchParams({
        state: state.value,
        csrf_token: csrf.value,
        action: "identity_chaining_check",
        client_id: card.getAttribute("data-remote-client"),
      }),
      headers: { Accept: "application/json" },
    })
      .then(function (response) {
        return response.ok ? response.json() : null;
      })
      .then(
        function (result) {
          settleChainCheck(status, card, result);
        },
        function () {
          settleChainCheck(status, card, null);
        },
      );
  });

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
