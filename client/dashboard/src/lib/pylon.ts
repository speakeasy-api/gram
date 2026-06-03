/**
 * Pylon widget initialization.
 *
 * Pylon's widget reads `window.pylon.chat_settings` once on script
 * execution to associate the visitor with their persisted thread history.
 * If chat_settings isn't present when the script runs, the visitor is
 * treated as anonymous and starts a fresh thread on every load —
 * which is why we must set chat_settings *before* injecting the script.
 *
 * The default launcher bubble is hidden via CSS — chat is triggered
 * from the "Support" button in the header instead.
 */

export const PYLON_APP_ID = "f9cade16-8d3c-4826-9a2a-034fad495102";

export type PylonChatSettings = {
  app_id: string;
  email: string;
  name: string;
  avatar_url?: string;
  email_hash?: string;
  hide_default_launcher?: boolean;
};

declare global {
  interface Window {
    Pylon: ((action: string, ...args: unknown[]) => void) & {
      q: unknown[];
      e: (args: unknown) => void;
    };
    pylon?: {
      chat_settings: PylonChatSettings;
    };
  }
}

let initialized = false;

/**
 * Initialize the Pylon widget. Idempotent — subsequent calls update
 * `window.pylon.chat_settings` so re-identification reflects the latest
 * user data but never injects a second script tag.
 */
export function initializePylon(chatSettings: PylonChatSettings): void {
  // Always keep chat_settings in sync with the latest user identity.
  window.pylon = { chat_settings: chatSettings };

  if (initialized) {
    return;
  }
  initialized = true;

  const style = document.createElement("style");
  style.textContent = `#pylon-chat-bubble { display: none !important; }`;
  document.head.appendChild(style);

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
