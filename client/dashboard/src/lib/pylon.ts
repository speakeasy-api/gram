/**
 * Pylon widget initialization
 * This module loads the Pylon chat widget script dynamically.
 * The default launcher bubble is hidden via CSS — chat is triggered
 * from the "Support" button in the header instead.
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
        hide_default_launcher?: boolean;
      };
    };
  }
}

/**
 * Initialize the Pylon widget by injecting the script tag
 */
export function initializePylon(): void {
  // Hide the default Pylon chat bubble so it doesn't overlap the
  // playground composer. Inject the style before the script loads.
  const style = document.createElement("style");
  style.textContent = `#pylon-chat-bubble { display: none !important; }`;
  document.head.appendChild(style);

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
