/**
 * Pylon widget initialization
 * This module loads the Pylon chat widget script dynamically
 */

const PYLON_APP_ID = "f9cade16-8d3c-4826-9a2a-034fad495102";

declare global {
  interface Window {
    Pylon: ((action: string, ...args: unknown[]) => void) & {
      q: unknown[];
      e: (args: unknown) => void;
    };
    pylon?: {
      chat_settings: {
        app_id: string;
        email: string;
        name: string;
        avatar_url?: string;
        email_hash?: string;
      };
    };
  }
}

/**
 * Initialize the Pylon widget by injecting the script tag
 */
export function initializePylon(): void {
  // Set up the Pylon queue before the script loads
  const queue: unknown[] = [];
  const enqueue = (args: unknown) => {
    queue.push(args);
  };

  const pylonFn = function (this: unknown, ...args: unknown[]) {
    enqueue(args);
  } as typeof window.Pylon;

  pylonFn.q = queue;
  pylonFn.e = enqueue;

  window.Pylon = pylonFn;

  // Load the Pylon script
  const script = document.createElement("script");
  script.setAttribute("type", "text/javascript");
  script.setAttribute("async", "true");
  script.setAttribute(
    "src",
    `https://widget.usepylon.com/widget/${PYLON_APP_ID}`,
  );

  const firstScript = document.getElementsByTagName("script")[0];
  firstScript?.parentNode?.insertBefore(script, firstScript);
}
