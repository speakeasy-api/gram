import { createRoot } from "react-dom/client";

import { ConsentToolsApp, type ConsentToolsAppProps } from "./ConsentToolsApp";
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
