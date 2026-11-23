import { createRoot } from "react-dom/client";

import { ConsentToolsApp, type ConsentToolsAppProps } from "./ConsentToolsApp";
import {
  ConsentInventoryFailure,
  loadConsentInventory,
  parseGatewayInventoryBootstrap,
  reportConsentInventoryFailure,
  RESTART_CONSENT_MESSAGE,
} from "./consentInventory";
import { parsePrefillBootstrap } from "./prefill";
import "./consent-tools.css";

const MOUNT_ID = "consent-tools-root";

function readBootstrap(el: HTMLElement): ConsentToolsAppProps | null {
  const toolsUrl = el.dataset["toolsUrl"];
  const state = el.dataset["state"];
  const csrfToken = el.dataset["csrfToken"];
  const formId = el.dataset["formId"];
  const approveButtonId = el.dataset["approveButtonId"];
  const consentEnabled = el.dataset["consentEnabled"];
  const inventoryDeferred = el.dataset["inventoryDeferred"];
  const serverName = el.dataset["serverName"];
  if (
    !toolsUrl ||
    !toolsUrl.startsWith("/") ||
    !state ||
    !csrfToken ||
    !formId ||
    !approveButtonId ||
    !serverName ||
    (consentEnabled !== "true" && consentEnabled !== "false")
  ) {
    return null;
  }
  let gatewayInventory: ConsentToolsAppProps["gatewayInventory"];
  if (el.dataset["gateway"] === "true") {
    const parsed = parseGatewayInventoryBootstrap(
      el.dataset["gatewayInventory"],
    );
    if (!parsed) return null;
    gatewayInventory = parsed;
  }
  return {
    toolsUrl,
    state,
    csrfToken,
    formId,
    approveButtonId,
    reconnectFormId: el.dataset["reconnectFormId"],
    flowID: el.dataset["flowId"],
    consentEnabled: consentEnabled === "true",
    gateway: el.dataset["gateway"] === "true",
    inventoryDeferred: inventoryDeferred === "true",
    ...(gatewayInventory ? { gatewayInventory } : {}),
    serverName,
    prefill: parsePrefillBootstrap(el.dataset["prefill"]),
  };
}

const mount = document.getElementById(MOUNT_ID);
if (mount) {
  const bootstrap = readBootstrap(mount);
  if (bootstrap) {
    createRoot(mount).render(<ConsentToolsApp {...bootstrap} />);
  } else {
    // Approval stays disabled (the server renders the button disabled and
    // only the app enables it), so a broken bootstrap fails closed.
    reportConsentInventoryFailure(
      new ConsentInventoryFailure("bootstrap", "missing_inventory"),
      mount.dataset["flowId"],
    );
    mount.setAttribute("role", "alert");
    mount.textContent =
      "Tool access could not be initialized. Reload the page to continue.";
  }
}

// The checking page uses the same bounded consent transport as the picker.
// Only the server decides whether the captured inventory matches approval.
const reuseForm = document.getElementById("consent-reuse");
if (reuseForm instanceof HTMLFormElement) {
  void checkSavedConsent(reuseForm);
}

async function checkSavedConsent(form: HTMLFormElement): Promise<void> {
  const fallback = form.dataset["fallback"];
  const toolsUrl = form.dataset["toolsUrl"];
  const state = form.elements.namedItem("state");
  const csrf = form.elements.namedItem("csrf_token");
  const inventory = form.elements.namedItem("tool_inventory_id");
  if (
    !fallback?.startsWith("/") ||
    !toolsUrl?.startsWith("/") ||
    !(state instanceof HTMLInputElement) ||
    !(csrf instanceof HTMLInputElement) ||
    !(inventory instanceof HTMLInputElement)
  )
    return;
  try {
    const { attempt } = await loadConsentInventory({
      toolsUrl,
      state: state.value,
      csrfToken: csrf.value,
      flowID: form.dataset["flowId"],
    });
    inventory.value = attempt;
    form.requestSubmit();
  } catch (err) {
    if (err instanceof ConsentInventoryFailure && err.recovery === "restart") {
      const status = document.querySelector("p[data-consent-auto-check]");
      if (status) {
        status.setAttribute("role", "alert");
        status.textContent = RESTART_CONSENT_MESSAGE;
      }
      document
        .querySelectorAll("h1 [data-consent-auto-check]")
        .forEach((element) => {
          if (element instanceof HTMLSpanElement)
            element.textContent = "Authorization expired";
          else element.remove();
        });
      return;
    }
    window.location.replace(fallback);
  }
}
