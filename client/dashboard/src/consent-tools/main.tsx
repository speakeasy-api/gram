import { createRoot } from "react-dom/client";

import { ConsentToolsApp, type ConsentToolsAppProps } from "./ConsentToolsApp";
import { fetchConsentInventory } from "./consentInventory";
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
  return {
    toolsUrl,
    state,
    csrfToken,
    formId,
    approveButtonId,
    consentEnabled: consentEnabled === "true",
    inventoryDeferred: inventoryDeferred === "true",
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
    const attempt = crypto.randomUUID();
    await fetchConsentInventory(
      toolsUrl,
      state.value,
      csrf.value,
      attempt,
      AbortSignal.timeout(10000),
    );
    inventory.value = attempt;
    form.requestSubmit();
  } catch {
    window.location.replace(fallback);
  }
}
