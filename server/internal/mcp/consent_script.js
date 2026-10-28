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
      // The row attribute swaps the hide icon for the Show label (see the
      // gatewayServerRow template).
      row.toggleAttribute("data-gateway-server-hidden", hide);
      var toggle = row.querySelector("[data-gateway-visibility-toggle]");
      toggle.value = hide ? "show_server" : "hide_server";
      toggle.title = hide
        ? "Make this server available to your tools again"
        : "Hide this server, preventing your agent from attempting to access it.";
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
    var agentInputs = document.querySelectorAll("input[data-agent-select]");
    var agentModes = document.querySelectorAll("input[data-agent-mode]");
    var agentMode = document.querySelector(
      'input[data-agent-mode][value="agent"]',
    );
    var selfMode = document.querySelector(
      'input[data-agent-mode][value="self"]',
    );
    var selfAgentInput = document.querySelector("[data-agent-self]");
    var agentPicker = document.querySelector("[data-agent-picker]");
    var agentSetup = document.querySelector("[data-agent-setup]");
    var agentSearch = document.querySelector("[data-agent-search]");
    var agentEmpty = document.querySelector("[data-agent-empty]");
    var agentPolicy = document.querySelector("[data-agent-policy]");
    var agentPolicyName = document.querySelector("[data-agent-policy-name]");
    var subjectDisplay = document.querySelector(
      "[data-consent-subject-display]",
    );
    var subjectMode = document.querySelector("[data-consent-subject-mode]");
    var selfOnlySections = document.querySelectorAll("[data-agent-self-only]");
    var services = document.querySelector("[data-service-connections]");
    var connectionRevision = 0;
    var activeConnectionAgent = "";
    var savedAgentKey = "gram-consent-agent-v1:" + form.elements.state.value;
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
    // This is UI state only. The server reauthorizes every selection and write.
    // sessionStorage survives a full-page upstream OAuth round trip in this tab.
    try {
      var savedAgent = sessionStorage.getItem(savedAgentKey);
      Array.prototype.forEach.call(agentInputs, function (input) {
        if (savedAgent !== null && input.value === savedAgent) {
          input.checked = true;
          if (agentMode) agentMode.checked = true;
        }
      });
    } catch (_) {
      /* Storage may be disabled; normal selection still works. */
    }

    async function connectionRequest(action, agentId, extra) {
      var body = new URLSearchParams({
        state: form.elements.state.value,
        csrf_token: form.elements.csrf_token.value,
        action: action,
        agent_id: agentId,
      });
      Object.keys(extra || {}).forEach(function (key) {
        body.set(key, extra[key]);
      });
      var response = await fetch(services.getAttribute("data-action-url"), {
        method: "POST",
        credentials: "same-origin",
        body: body,
        headers: { Accept: "application/json" },
      });
      if (!response.ok) {
        var error = new Error(
          "Could not verify agent access (" +
            response.status +
            "). Retry, or restart authorization if the page has expired.",
        );
        error.status = response.status;
        throw error;
      }
      return response.json();
    }

    async function updateAgentAccess(agentId) {
      activeConnectionAgent = agentId;
      var revision = ++connectionRevision;
      if (!services) {
        if (agentId) button.disabled = submitted;
        return;
      }
      var providers = services.querySelectorAll("[data-remote-client]");
      Array.prototype.forEach.call(providers, function (provider) {
        var access = provider.querySelector("[data-agent-access]");
        if (!access) return;
        access.hidden = !agentId;
        access.replaceChildren();
        if (agentId) access.textContent = "Checking access for this agent…";
      });
      if (!agentId) return;
      button.disabled = true;
      function current() {
        return (
          revision === connectionRevision && activeConnectionAgent === agentId
        );
      }
      function showError(error) {
        if (!current()) return;
        button.disabled = true;
        Array.prototype.forEach.call(providers, function (provider) {
          var access = provider.querySelector("[data-agent-access]");
          if (!access) return;
          access.replaceChildren();
          var message = document.createElement("p");
          message.className = "text-muted-foreground text-xs";
          message.setAttribute("role", "alert");
          message.textContent =
            error.message +
            " Connection state is unknown. Retry to reload the current state.";
          access.appendChild(message);
          if (error.status === 401 || error.status === 403) {
            var unavailable = document.createElement("p");
            unavailable.setAttribute("data-agent-access-unavailable", "");
            unavailable.className = "text-muted-foreground text-xs";
            unavailable.textContent =
              "This agent is not available for authorization by the signed-in user. Select another identity or restart authorization.";
            access.appendChild(unavailable);
          }
          var retry = document.createElement("button");
          retry.type = "button";
          retry.className =
            "bg-card interact:bg-accent h-10 border px-4 text-sm";
          retry.textContent = "Retry agent access";
          retry.addEventListener("click", function () {
            updateAgentAccess(agentId);
          });
          access.appendChild(retry);
        });
      }
      async function mutate(action, extra) {
        if (!current()) return;
        button.disabled = true;
        Array.prototype.forEach.call(
          services.querySelectorAll(
            "[data-agent-access] button, [data-agent-access] select",
          ),
          function (control) {
            control.disabled = true;
          },
        );
        try {
          await connectionRequest(action, agentId, extra);
          if (current()) await updateAgentAccess(agentId);
        } catch (error) {
          // Do not retry writes automatically: the response may have been lost
          // after commit. A fresh read reconciles state when the user retries.
          showError(error);
        }
      }
      function connectionUsable(session) {
        if (!session) return false;
        if (
          session.HasRefreshToken &&
          (!session.RefreshExpiresAt ||
            new Date(session.RefreshExpiresAt).getTime() > Date.now())
        )
          return true;
        return (
          !session.AccessExpiresAt ||
          new Date(session.AccessExpiresAt).getTime() > Date.now()
        );
      }
      function accountLabel(session) {
        return (
          [session.UpstreamDisplayName, session.UpstreamEmail]
            .filter(function (value) {
              return typeof value === "string" && value.trim() !== "";
            })
            .join(" · ") || "Identity unavailable"
        );
      }
      if (!providers.length) {
        button.disabled = submitted;
        return;
      }
      try {
        var data = await connectionRequest("agent_connections", agentId);
        if (!current()) return;
        while (data.nextCursor) {
          var page = await connectionRequest("agent_connections", agentId, {
            cursor: data.nextCursor,
          });
          if (!current()) return;
          data.candidates = (data.candidates || []).concat(
            page.candidates || [],
          );
          data.nextCursor = page.nextCursor;
        }
        // Advisory only: final approval runs the runtime credential resolver.
        button.disabled =
          submitted ||
          !Array.prototype.every.call(providers, function (provider) {
            return (data.bindings || []).some(function (binding) {
              return (
                binding.RemoteSessionClientID ===
                  provider.getAttribute("data-remote-client") &&
                connectionUsable(binding.RemoteSession)
              );
            });
          });
        Array.prototype.forEach.call(providers, function (provider) {
          var clientId = provider.getAttribute("data-remote-client");
          var display = provider.getAttribute("data-remote-display");
          var row = provider.querySelector("[data-agent-access]");
          if (!row) return;
          row.replaceChildren();
          var binding = (data.bindings || []).find(function (item) {
            return item.RemoteSessionClientID === clientId;
          });
          if (binding) {
            var attached = document.createElement("span");
            attached.className = "text-muted-foreground text-xs";
            var account = binding.RemoteSession;
            var eligible = connectionUsable(account);
            var connectedAs = provider.getAttribute("data-connected-as");
            var sameAccount =
              account &&
              connectedAs &&
              (connectedAs === accountLabel(account) ||
                connectedAs === account.UpstreamEmail ||
                connectedAs === account.UpstreamDisplayName);
            attached.textContent = eligible
              ? "Available to agent" +
                (sameAccount ? "" : " · " + accountLabel(account))
              : "Agent access unavailable. Detach it before choosing an account. Your service connection has not been changed.";
            row.appendChild(attached);
            if (!data.canManageConnections) {
              var readonly = document.createElement("span");
              readonly.textContent =
                "Only agent editors can change this assignment.";
              row.appendChild(readonly);
              return;
            }
            var detach = document.createElement("button");
            detach.type = "button";
            detach.className =
              "bg-card interact:bg-accent h-10 border px-4 text-sm";
            detach.textContent = "Remove agent access";
            detach.addEventListener("click", function () {
              mutate("agent_detach", { binding_id: binding.ID });
            });
            row.appendChild(detach);
          } else {
            if (!data.canManageConnections) {
              row.textContent = "Ask an agent editor to assign a connection.";
              return;
            }
            var candidates = (data.candidates || []).filter(function (item) {
              return item.RemoteSessionClientID === clientId;
            });
            if (candidates.length) {
              var select = document.createElement("select");
              select.setAttribute(
                "aria-label",
                "Your " + display + " connection",
              );
              select.className = "bg-card h-10 border px-4 text-sm";
              candidates.forEach(function (candidate) {
                var option = document.createElement("option");
                option.value = candidate.ID;
                option.textContent =
                  accountLabel(candidate) +
                  ((candidate.Scopes || []).length
                    ? " — " + candidate.Scopes.join(", ")
                    : "");
                select.appendChild(option);
              });
              // The provider card already identifies its connected account.
              // Show a chooser only for a choice or a different eligible account.
              var only = candidates[0];
              var shown = provider.getAttribute("data-connected-as");
              if (
                candidates.length > 1 ||
                !shown ||
                (shown !== accountLabel(only) &&
                  shown !== only.UpstreamEmail &&
                  shown !== only.UpstreamDisplayName)
              ) {
                row.appendChild(select);
              }
              var notice = document.createElement("span");
              notice.className = "text-muted-foreground text-xs";
              notice.textContent = "Not yet available to agent";
              row.appendChild(notice);
              var attach = document.createElement("button");
              attach.type = "button";
              attach.className =
                "bg-card interact:bg-accent h-10 border px-4 text-sm";
              attach.textContent = "Use this account";
              attach.addEventListener("click", function () {
                mutate("agent_attach", { remote_session_id: select.value });
              });
              row.appendChild(attach);
            } else {
              var missing = document.createElement("span");
              missing.className = "text-muted-foreground text-xs";
              missing.textContent =
                "No account is eligible for this agent. Connect or reconnect this service, then choose Use this account. An existing connection may not meet this agent’s permissions.";
              row.appendChild(missing);
            }
          }
        });
      } catch (error) {
        showError(error);
      }
    }
    if (agentInputs.length > 0) {
      var syncAgentSelection = function () {
        var authorizingAgent = Boolean(agentMode && agentMode.checked);
        var selected = authorizingAgent ? null : selfMode;
        Array.prototype.forEach.call(agentInputs, function (input) {
          input.disabled = !authorizingAgent;
          input.required = authorizingAgent;
          if (!authorizingAgent) input.checked = false;
          if (input.checked) selected = input;
        });
        if (selfAgentInput) selfAgentInput.disabled = authorizingAgent;
        if (agentPicker) agentPicker.hidden = !authorizingAgent;
        if (agentSetup) agentSetup.hidden = !authorizingAgent;
        var selectedDisplay = selected
          ? selected.getAttribute("data-subject-display") || ""
          : "";
        if (agentPolicy) {
          agentPolicy.hidden = !authorizingAgent || !selected;
        }
        if (agentPolicyName) {
          agentPolicyName.textContent = authorizingAgent ? selectedDisplay : "";
        }
        Array.prototype.forEach.call(selfOnlySections, function (section) {
          section.hidden = authorizingAgent;
        });
        var discoveryMode = document.querySelector(
          'select[name="discovery_mode"]',
        );
        if (discoveryMode) {
          discoveryMode.disabled = authorizingAgent;
        }
        if (subjectDisplay) {
          subjectDisplay.textContent = selectedDisplay || "Choose an agent";
        }
        if (subjectMode) {
          subjectMode.textContent = authorizingAgent
            ? "Authorizing"
            : "Signing in as";
        }
        if (button) {
          button.textContent = authorizingAgent
            ? button.getAttribute("data-agent-label")
            : button.getAttribute("data-self-label");
          button.setAttribute(
            "data-agent-selected",
            authorizingAgent ? "true" : "false",
          );
          button.value = authorizingAgent ? "approve_agent" : "approve";
          button.disabled = authorizingAgent
            ? true
            : button.getAttribute("data-consent-self-ready") !== "true";
        }
        var selectedID = authorizingAgent && selected ? selected.value : "";
        try {
          sessionStorage.setItem(savedAgentKey, selectedID);
        } catch (_) {}
        updateAgentAccess(selectedID);
      };
      Array.prototype.forEach.call(agentInputs, function (input) {
        input.addEventListener("change", syncAgentSelection);
      });
      Array.prototype.forEach.call(agentModes, function (input) {
        input.addEventListener("change", syncAgentSelection);
      });
      if (agentSearch) {
        agentSearch.addEventListener("input", function () {
          var query = agentSearch.value.trim().toLowerCase();
          var matches = 0;
          Array.prototype.forEach.call(agentInputs, function (input) {
            var matchesName = (input.getAttribute("data-subject-display") || "")
              .toLowerCase()
              .includes(query);
            input.closest("[data-agent-option]").hidden = !matchesName;
            if (matchesName) matches++;
          });
          if (agentEmpty) agentEmpty.hidden = matches !== 0;
        });
      }
      syncAgentSelection();
      // Agent mode requires these handlers; leave it unavailable if the script
      // cannot load or initialize rather than silently approving as Myself.
      if (agentMode) agentMode.disabled = false;
    }

    form.addEventListener("submit", function (event) {
      // Guard implicit/programmatic submissions too: an empty Agent choice
      // must never fall through to self authorization.
      if (
        submitted ||
        (button && button.disabled) ||
        (agentMode &&
          agentMode.checked &&
          !Array.prototype.some.call(agentInputs, function (input) {
            return input.checked && !input.disabled;
          }))
      ) {
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
