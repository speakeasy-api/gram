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
          savedMode === "progressive"
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
        if (savedAgent !== null && input.value === savedAgent)
          input.checked = true;
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
            var login = document.createElement("p");
            login.setAttribute("data-agent-access-login", "");
            login.className = "text-muted-foreground text-xs";
            login.textContent =
              "Sign in to Gram as the authorizing user in this organization, then retry.";
            access.appendChild(login);
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
                binding.RemoteSession &&
                (data.candidates || []).some(function (session) {
                  return session.ID === binding.RemoteSessionID;
                })
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
            var eligible =
              account &&
              (data.candidates || []).some(function (candidate) {
                return candidate.ID === binding.RemoteSessionID;
              });
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
        var selected = null;
        Array.prototype.forEach.call(agentInputs, function (input) {
          if (input.checked) {
            selected = input;
          }
        });
        var authorizingAgent = Boolean(selected && selected.value !== "");
        var selectedDisplay = selected
          ? selected.getAttribute("data-subject-display") || ""
          : "";
        if (agentPolicy) {
          agentPolicy.hidden = !authorizingAgent;
        }
        if (agentPolicyName) {
          agentPolicyName.textContent = authorizingAgent ? selectedDisplay : "";
        }
        Array.prototype.forEach.call(selfOnlySections, function (section) {
          section.hidden = authorizingAgent;
        });
        document
          .querySelectorAll(
            'select[name="discovery_mode"], input[name="gateway_freeze"], input[name="gateway_tools"]',
          )
          .forEach(function (input) {
            input.disabled =
              authorizingAgent || input.hasAttribute("data-unavailable");
          });
        if (subjectDisplay && selected) {
          subjectDisplay.textContent = selectedDisplay;
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
        var selectedID = authorizingAgent ? selected.value : "";
        try {
          sessionStorage.setItem(savedAgentKey, selectedID);
        } catch (_) {}
        updateAgentAccess(selectedID);
      };
      Array.prototype.forEach.call(agentInputs, function (input) {
        input.addEventListener("change", syncAgentSelection);
      });
      syncAgentSelection();
    }

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
