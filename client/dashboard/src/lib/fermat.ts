/**
 * Claire de Fermat pixel initialization.
 *
 * Fermat reads from the `window.fermat.q` command queue once its async
 * script finishes loading. We stub `window.fermat` as a queue-pusher
 * before injecting the script so any `init`/`track` calls made during
 * load are buffered and replayed — mirroring the snippet Fermat ships.
 */

const FERMAT_SCRIPT_SRC = "https://static.clairedefermat.com/pixel/v2/pixel.js";
const FERMAT_SOURCE_ID = "speakeasy-com-6f500c49";

type FermatCommand =
  | {
      method: "init";
      config: {
        id: string;
        sourceType: string;
        metadata?: { properties?: Record<string, unknown> };
      };
    }
  | {
      method: "track";
      eventName: string;
      properties?: Record<string, unknown>;
    }
  | {
      method: "setProperties";
      properties: Record<string, unknown>;
    };

type FermatFn = ((command: FermatCommand) => void) & {
  q?: FermatCommand[];
};

declare global {
  interface Window {
    fermat?: FermatFn;
  }
}

let initialized = false;

/**
 * Initialize the Fermat pixel. Idempotent — subsequent calls are no-ops
 * so we never inject a second script tag (e.g. under React StrictMode or
 * component remounts).
 */
export function initializeFermat(): void {
  if (initialized) {
    return;
  }
  initialized = true;

  const fermatFn = function (command: FermatCommand) {
    (fermatFn.q = fermatFn.q || []).push(command);
  } as FermatFn;
  window.fermat = window.fermat || fermatFn;

  const script = document.createElement("script");
  script.src = FERMAT_SCRIPT_SRC;
  script.async = true;

  const firstScript = document.getElementsByTagName("script")[0];
  firstScript?.parentNode?.insertBefore(script, firstScript);

  window.fermat({
    method: "init",
    config: {
      id: FERMAT_SOURCE_ID,
      sourceType: "external-b2b",
      metadata: {
        properties: {
          app_name: "Speakeasy",
        },
      },
    },
  });
}

/**
 * Send a Fermat tracking event. No-op until `initializeFermat` has run.
 */
export function trackFermatEvent(
  eventName: string,
  properties?: Record<string, unknown>,
): void {
  window.fermat?.({ method: "track", eventName, properties });
}

/**
 * Attach stable user/account identifiers so Fermat can attribute activity
 * across sessions. Call after login or user hydration. No-op until
 * `initializeFermat` has run.
 */
export function setFermatProperties(properties: Record<string, unknown>): void {
  window.fermat?.({ method: "setProperties", properties });
}
